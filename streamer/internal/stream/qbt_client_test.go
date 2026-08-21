package stream

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestParseMagnetInfohash(t *testing.T) {
	const hexHash = "aabbccddeeff00112233445566778899aabbccdd"
	rawBytes, err := hex.DecodeString(hexHash)
	if err != nil {
		t.Fatal(err)
	}
	base32Hash := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawBytes)

	tests := []struct {
		name    string
		magnet  string
		want    string
		wantErr bool
	}{
		{"hex uppercase", "magnet:?xt=urn:btih:" + strings.ToUpper(hexHash), hexHash, false},
		{"base32", "magnet:?xt=urn:btih:" + base32Hash, hexHash, false},
		{"missing xt", "magnet:?dn=foo", "", true},
		{"wrong length", "magnet:?xt=urn:btih:abc123", "", true},
		{"invalid base32 chars", "magnet:?xt=urn:btih:11111111111111111111111111111111", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMagnetInfohash(tc.magnet)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapPath(t *testing.T) {
	t.Run("valid prefix substitution", func(t *testing.T) {
		got, err := mapPath("/data/downloads/Movie", "/data/downloads", "/mnt/qbit")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join("/mnt/qbit", "Movie")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("prefix mismatch is an explicit error", func(t *testing.T) {
		if _, err := mapPath("/other/path/Movie", "/data/downloads", "/mnt/qbit"); err == nil {
			t.Fatal("expected error for prefix mismatch")
		}
	})
	t.Run("missing config is an explicit error", func(t *testing.T) {
		if _, err := mapPath("/data/downloads/Movie", "", "/mnt/qbit"); err == nil {
			t.Fatal("expected error for missing remoteRoot")
		}
	})
}

func TestNewQBitClientWithAPI_MissingDownloadDir(t *testing.T) {
	fake := newFakeQbtAPI()
	if _, err := newQBitClientWithAPI(fake, "/remote", "/nonexistent/path/xyz", "cat", time.Second, time.Minute); err == nil {
		t.Fatal("expected error for missing download dir")
	}
}

func TestNewQBitClientWithAPI_LoginFailure(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.loginErr = errors.New("bad credentials")
	if _, err := newQBitClientWithAPI(fake, "/remote", t.TempDir(), "cat", time.Second, time.Minute); err == nil {
		t.Fatal("expected login failure to be fatal")
	}
}

func TestNewQBitClientWithAPI_PurgeFailureIsFatal(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.getTorrentsErr = errors.New("qbittorrent unreachable")
	if _, err := newQBitClientWithAPI(fake, "/remote", t.TempDir(), "cat", time.Second, time.Minute); err == nil {
		t.Fatal("expected purge failure to be fatal")
	}
}

func TestNewQBitClientWithAPI_PurgesExistingCategory(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["orphan1"] = qbt.Torrent{Hash: "orphan1", Category: "cat"}
	fake.torrents["other"] = qbt.Torrent{Hash: "other", Category: "different-cat"}

	if _, err := newQBitClientWithAPI(fake, "/remote", t.TempDir(), "cat", time.Second, time.Minute); err != nil {
		t.Fatalf("newQBitClientWithAPI: %v", err)
	}
	if len(fake.deleteCalls) != 1 || len(fake.deleteCalls[0]) != 1 || fake.deleteCalls[0][0] != "orphan1" {
		t.Errorf("expected purge to delete only orphan1, got %v", fake.deleteCalls)
	}
}

// TestQbtFile_NewReader_TriesDownloadPathBeforeSavePath covers the exact
// scenario that caused a real streaming failure: qBittorrent's "Keep
// incomplete torrents in a different folder" option means a still-downloading
// file physically lives under download_path, not save_path, even though
// save_path is what most naive integrations assume is the only location.
func TestQbtFile_NewReader_TriesDownloadPathBeforeSavePath(t *testing.T) {
	fake := newFakeQbtAPI()
	remoteRoot := "/data/downloads"
	downloadDir := t.TempDir()

	incompleteDir := filepath.Join(downloadDir, "incomplete", "Movie")
	if err := os.MkdirAll(incompleteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incompleteDir, "a.mp4"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	tor := &qbtTorrent{
		hash: "zz", api: fake, downloadDir: downloadDir, remoteRoot: remoteRoot,
		pollInterval: 2 * time.Millisecond, refcounts: make(map[int]int),
		savePath:     "/data/downloads/Movie",            // final destination — file not there yet
		downloadPath: "/data/downloads/incomplete/Movie", // where it actually is right now
		pieceSize:    1024,
	}
	f := &qbtFile{
		hash: "zz", index: 0, name: "a.mp4", size: 5,
		api: fake, downloadDir: downloadDir, remoteRoot: remoteRoot,
		pollInterval: 2 * time.Millisecond, torrent: tor,
	}
	fake.setPieceStates("zz", []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	r := f.NewReader()
	defer r.Close()

	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Errorf("Read = %d %q, want 5 bytes \"hello\"", n, buf)
	}
}

// TestQbtFile_NewReader_FallsBackToSavePathOnceComplete covers the other half
// of the same torrent's lifecycle: once complete, qBittorrent has moved the
// file out of download_path into save_path, and a fresh reader (e.g. a later
// request) must still find it there.
func TestQbtFile_NewReader_FallsBackToSavePathOnceComplete(t *testing.T) {
	fake := newFakeQbtAPI()
	remoteRoot := "/data/downloads"
	downloadDir := t.TempDir()

	finalDir := filepath.Join(downloadDir, "Movie")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "a.mp4"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	tor := &qbtTorrent{
		hash: "yy", api: fake, downloadDir: downloadDir, remoteRoot: remoteRoot,
		pollInterval: 2 * time.Millisecond, refcounts: make(map[int]int),
		savePath:     "/data/downloads/Movie",
		downloadPath: "/data/downloads/incomplete/Movie", // no longer has the file
		pieceSize:    1024,
	}
	f := &qbtFile{
		hash: "yy", index: 0, name: "a.mp4", size: 5,
		api: fake, downloadDir: downloadDir, remoteRoot: remoteRoot,
		pollInterval: 2 * time.Millisecond, torrent: tor,
	}
	fake.setPieceStates("yy", []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	r := f.NewReader()
	defer r.Close()

	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Errorf("Read = %d %q, want 5 bytes \"hello\"", n, buf)
	}
}

func TestQbtTorrent_MetadataReady(t *testing.T) {
	fake := newFakeQbtAPI()
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fake.props[hash] = qbt.TorrentProperties{PiecesNum: 3, Name: "Movie", SavePath: "/data/downloads/Movie", PieceSize: 1024}
	fake.files[hash] = qbt.TorrentFiles{
		{Index: 1, Name: "b.mp4", Size: 500, Progress: 0},
		{Index: 0, Name: "a.mp4", Size: 2048, Progress: 0.5},
	}

	c := &qbtClient{api: fake, downloadDir: "/local", remoteRoot: "/data/downloads", category: "cat", pollInterval: 2 * time.Millisecond, idleTimeout: 50 * time.Millisecond}
	tor, err := c.AddMagnet("magnet:?xt=urn:btih:" + hash)
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}

	select {
	case <-tor.GotInfo():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("GotInfo did not close in time")
	}

	if tor.Name() != "Movie" {
		t.Errorf("Name() = %q, want %q", tor.Name(), "Movie")
	}
	files := tor.Files()
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path() != "a.mp4" || files[1].Path() != "b.mp4" {
		t.Errorf("files not sorted by index: %q, %q", files[0].Path(), files[1].Path())
	}
	f0, f1 := files[0].(*qbtFile), files[1].(*qbtFile)
	if f0.fileOffset != 0 {
		t.Errorf("file 0 offset = %d, want 0", f0.fileOffset)
	}
	if f1.fileOffset != 2048 {
		t.Errorf("file 1 offset = %d, want 2048", f1.fileOffset)
	}

	// Every file should have been zeroed out at metadata-ready time (Decision #6:
	// nothing downloads by default; PrioritizeFile later promotes exactly one).
	calls := fake.getPriorityCalls()
	if len(calls) != 1 || calls[0].priority != 0 || (calls[0].ids != "0|1" && calls[0].ids != "1|0") {
		t.Errorf("expected one zero-all-priorities call, got %v", calls)
	}
}

func TestQbtTorrent_PrioritizeFile_DedupAndDeferredDemotion(t *testing.T) {
	fake := newFakeQbtAPI()
	tor := &qbtTorrent{
		hash: "bb", api: fake, pollInterval: 2 * time.Millisecond, idleTimeout: 200 * time.Millisecond,
		stopCh: make(chan struct{}), gotInfo: make(chan struct{}), refcounts: make(map[int]int),
	}

	tor.PrioritizeFile(0)
	tor.PrioritizeFile(0) // repeat call for the same index must not re-issue the API call

	calls := fake.getPriorityCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 priority call after dedup, got %d: %v", len(calls), calls)
	}

	// Simulate an open reader on file 0 before switching to file 1.
	tor.acquireReader(0)
	tor.PrioritizeFile(1)

	// Demotion of file 0 must be deferred while its reader is open.
	time.Sleep(20 * time.Millisecond)
	for _, c := range fake.getPriorityCalls() {
		if c.ids == "0" && c.priority == 0 {
			t.Fatal("file 0 was demoted while its reader was still open")
		}
	}

	// Releasing the reader must trigger immediate demotion.
	tor.releaseReader(0)
	deadline := time.After(500 * time.Millisecond)
	for {
		found := false
		for _, c := range fake.getPriorityCalls() {
			if c.ids == "0" && c.priority == 0 {
				found = true
			}
		}
		if found {
			break
		}
		select {
		case <-deadline:
			t.Fatal("expected file 0 to be demoted after its reader closed")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestQbtTorrent_DemoteNow_SelfCorrectsIfRacinglyRepromoted covers the TOCTOU
// race between a staleness check (in tryDemote/releaseReader) and demoteNow's
// network call actually landing: if index has been re-picked by the time the
// demote call completes, demoteNow must re-promote it rather than stranding it
// at priority 0 (a repeat PrioritizeFile(index) call would otherwise be a
// no-op dedup and never fix it).
func TestQbtTorrent_DemoteNow_SelfCorrectsIfRacinglyRepromoted(t *testing.T) {
	fake := newFakeQbtAPI()
	tor := &qbtTorrent{
		hash: "hh", api: fake, refcounts: make(map[int]int),
		prioritized: 0, hasPrioritized: true, // index 0 is (again) the current pick
	}

	tor.demoteNow(0)

	calls := fake.getPriorityCalls()
	if len(calls) != 2 {
		t.Fatalf("expected demote then self-correcting re-promote, got %d calls: %v", len(calls), calls)
	}
	if calls[0].priority != 0 {
		t.Errorf("first call should be the demote, got priority %d", calls[0].priority)
	}
	if calls[1].priority != 1 {
		t.Errorf("second call should be the re-promote, got priority %d", calls[1].priority)
	}
}

func TestQbtTorrent_DemoteNow_NoSelfCorrectWhenStillStale(t *testing.T) {
	fake := newFakeQbtAPI()
	tor := &qbtTorrent{
		hash: "ii", api: fake, refcounts: make(map[int]int),
		prioritized: 1, hasPrioritized: true, // index 0 really is stale
	}

	tor.demoteNow(0)

	calls := fake.getPriorityCalls()
	if len(calls) != 1 || calls[0].priority != 0 {
		t.Errorf("expected only the demote call, got %v", calls)
	}
}

func TestQbtTorrent_NotifyGone_FiresOnceEvenConcurrently(t *testing.T) {
	tor := &qbtTorrent{hash: "mm", refcounts: make(map[int]int)}
	var mu sync.Mutex
	var calls int
	tor.SetGoneCallback(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tor.notifyGone()
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected the gone-callback to fire exactly once, got %d", calls)
	}
}

// --- docs/STREAMING.md §7: Pause/Resume/TagClientID + clientLister ---

func TestQbtTorrent_PauseResume(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["cc"] = qbt.Torrent{Hash: "cc", State: qbt.TorrentStateDownloading}
	tor := &qbtTorrent{hash: "cc", api: fake, refcounts: make(map[int]int)}

	if err := tor.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if fake.torrents["cc"].State != qbt.TorrentStatePausedDl {
		t.Errorf("expected paused state, got %v", fake.torrents["cc"].State)
	}

	if err := tor.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if fake.torrents["cc"].State != qbt.TorrentStateDownloading {
		t.Errorf("expected downloading state after resume, got %v", fake.torrents["cc"].State)
	}
}

func TestQbtTorrent_TagClientID(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["cc"] = qbt.Torrent{Hash: "cc"}
	tor := &qbtTorrent{hash: "cc", api: fake, refcounts: make(map[int]int)}

	tor.TagClientID("browser-1")
	calls := fake.getTagsCalls()
	if len(calls) != 1 || calls[0].tags != "browser-1" || calls[0].hashes[0] != "cc" {
		t.Errorf("expected one AddTagsCtx call for browser-1, got %v", calls)
	}

	tor.TagClientID("") // empty clientID must not call the API at all
	if len(fake.getTagsCalls()) != 1 {
		t.Error("empty clientID should not add a tag")
	}
}

func TestQbtClient_ListTorrents(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["a"] = qbt.Torrent{Hash: "a", Name: "Mine", Category: "tsa-stream-engine", Tags: "browser-1", State: qbt.TorrentStatePausedDl, Progress: 0.5}
	fake.torrents["b"] = qbt.Torrent{Hash: "b", Name: "Theirs", Category: "tsa-stream-engine", Tags: "browser-2"}
	c := &qbtClient{api: fake, category: "tsa-stream-engine"}

	got, err := c.ListTorrents(t.Context(), "browser-1")
	if err != nil {
		t.Fatalf("ListTorrents: %v", err)
	}
	if len(got) != 1 || got[0].Hash != "a" || !got[0].Paused {
		t.Errorf("expected only browser-1's paused torrent, got %+v", got)
	}
}

func TestQbtClient_ResumeDeleteMove_VerifyOwnership(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["a"] = qbt.Torrent{Hash: "a", Tags: "browser-1"}
	c := &qbtClient{api: fake, category: "tsa-stream-engine"}
	ctx := t.Context()

	if err := c.ResumeTorrent(ctx, "a", "browser-2"); !errors.Is(err, ErrTorrentNotFound) {
		t.Errorf("wrong owner: expected ErrTorrentNotFound, got %v", err)
	}
	if err := c.ResumeTorrent(ctx, "a", "browser-1"); err != nil {
		t.Errorf("correct owner Resume: %v", err)
	}

	if err := c.DeleteTorrent(ctx, "a", "browser-2"); !errors.Is(err, ErrTorrentNotFound) {
		t.Errorf("wrong owner: expected ErrTorrentNotFound, got %v", err)
	}
	if len(fake.deleteCalls) != 0 {
		t.Error("delete must not be called for the wrong owner")
	}

	if err := c.MoveToCategory(ctx, "a", "browser-1", "tsa-download"); err != nil {
		t.Fatalf("MoveToCategory: %v", err)
	}
	calls := fake.getCategoryCalls()
	if len(calls) != 1 || calls[0].category != "tsa-download" || calls[0].hashes[0] != "a" {
		t.Errorf("expected one SetCategoryCtx call to tsa-download, got %v", calls)
	}
}

func TestQbtClient_FlushCategory(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["a"] = qbt.Torrent{Hash: "a", Category: "tsa-stream-engine", Tags: "browser-1"}
	fake.torrents["b"] = qbt.Torrent{Hash: "b", Category: "tsa-stream-engine", Tags: "browser-2"}
	c := &qbtClient{api: fake, category: "tsa-stream-engine"}

	removed, err := c.FlushCategory(t.Context(), "browser-1")
	if err != nil {
		t.Fatalf("FlushCategory: %v", err)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Errorf("expected only browser-1's torrent removed, got %v", removed)
	}
	if len(fake.deleteCalls) != 1 || len(fake.deleteCalls[0]) != 1 || fake.deleteCalls[0][0] != "a" {
		t.Errorf("expected exactly one delete call for hash a, got %v", fake.deleteCalls)
	}
}

func TestQbtTorrent_NotifyGone_NoCallbackRegisteredIsSafe(t *testing.T) {
	tor := &qbtTorrent{hash: "nn", refcounts: make(map[int]int)}
	tor.notifyGone() // must not panic when no callback was ever registered
}

func TestQbtTorrent_PrioritizeFile_BoundedGracePeriod(t *testing.T) {
	fake := newFakeQbtAPI()
	tor := &qbtTorrent{
		hash: "cc", api: fake, pollInterval: 2 * time.Millisecond, idleTimeout: 20 * time.Millisecond,
		stopCh: make(chan struct{}), gotInfo: make(chan struct{}), refcounts: make(map[int]int),
	}

	tor.PrioritizeFile(0)
	tor.acquireReader(0) // never released — simulates a leaked blocked Read (client disconnect)
	tor.PrioritizeFile(1)

	// Even without releaseReader ever being called, the bounded grace period must
	// still force-demote file 0 rather than leaving it downloading forever.
	deadline := time.After(500 * time.Millisecond)
	for {
		found := false
		for _, c := range fake.getPriorityCalls() {
			if c.ids == "0" && c.priority == 0 {
				found = true
			}
		}
		if found {
			break
		}
		select {
		case <-deadline:
			t.Fatal("file 0 was never force-demoted after the grace period elapsed")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
