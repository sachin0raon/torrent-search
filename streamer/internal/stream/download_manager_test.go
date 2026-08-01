package stream

import (
	"context"
	"errors"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestNewDownloadManagerWithAPI_LoginFailure(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.loginErr = errors.New("bad credentials")
	if _, err := newDownloadManagerWithAPI(fake, "/remote", t.TempDir(), "tsa-download", time.Second); err == nil {
		t.Fatal("expected login failure to be fatal")
	}
}

func TestNewDownloadManagerWithAPI_MissingDownloadDir(t *testing.T) {
	fake := newFakeQbtAPI()
	if _, err := newDownloadManagerWithAPI(fake, "/remote", "/nonexistent/path/xyz", "tsa-download", time.Second); err == nil {
		t.Fatal("expected error for missing download dir")
	}
}

// TestNewDownloadManagerWithAPI_DoesNotPurge is the key behavioral difference
// from NewQBitClient (Decision #22): downloads must survive a streamer
// restart, so construction must never delete pre-existing category-tagged
// torrents the way the streaming engine's startup purge does.
func TestNewDownloadManagerWithAPI_DoesNotPurge(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["existing"] = qbt.Torrent{Hash: "existing", Category: "tsa-download"}

	if _, err := newDownloadManagerWithAPI(fake, "/remote", t.TempDir(), "tsa-download", time.Second); err != nil {
		t.Fatalf("newDownloadManagerWithAPI: %v", err)
	}
	if len(fake.deleteCalls) != 0 {
		t.Errorf("expected no deletes on construction, got %v", fake.deleteCalls)
	}
}

func newTestDownloadManager(t *testing.T, fake *fakeQbtAPI) *DownloadManager {
	t.Helper()
	m, err := newDownloadManagerWithAPI(fake, "/data/downloads", t.TempDir(), "tsa-download", 2*time.Millisecond)
	if err != nil {
		t.Fatalf("newDownloadManagerWithAPI: %v", err)
	}
	return m
}

func TestDownloadManager_AddTorrent_ZerosAllPrioritiesOnceReady(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)

	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fake.props[hash] = qbt.TorrentProperties{PiecesNum: 3, Name: "Movie", SavePath: "/data/downloads/Movie", PieceSize: 1024}
	fake.files[hash] = qbt.TorrentFiles{
		{Index: 1, Name: "b.mp4", Size: 500},
		{Index: 0, Name: "a.mp4", Size: 2048},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := m.AddTorrent(ctx, "magnet:?xt=urn:btih:"+hash)
	if err != nil {
		t.Fatalf("AddTorrent: %v", err)
	}
	if info.Hash != hash || info.Name != "Movie" {
		t.Errorf("unexpected info: %+v", info)
	}
	if len(info.Files) != 2 || info.Files[0].Name != "a.mp4" || info.Files[1].Name != "b.mp4" {
		t.Errorf("files not sorted by index: %+v", info.Files)
	}

	// Nothing should download until an explicit SelectFiles call.
	calls := fake.getPriorityCalls()
	if len(calls) != 1 || calls[0].priority != 0 || (calls[0].ids != "0|1" && calls[0].ids != "1|0") {
		t.Errorf("expected one zero-all-priorities call, got %v", calls)
	}

	if len(fake.added) != 1 {
		t.Fatalf("expected one add-torrent call, got %d", len(fake.added))
	}
}

// TestDownloadManager_AddTorrent_AlreadyTrackedSkipsReAdd covers the
// season-pack bug: picking a second file from a magnet whose torrent is
// already known to qBittorrent (added earlier for a different file in the
// same pack) must not re-call AddTorrentFromUrlCtx — qBittorrent rejects a
// duplicate add with an HTTP 409 ("conflicts detected"), which previously
// surfaced as a hard failure. It also must not zero the file priorities a
// prior SelectFiles call already set, or the first file's in-flight download
// would be silently stopped.
func TestDownloadManager_AddTorrent_AlreadyTrackedSkipsReAdd(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)

	const hash = "cccccccccccccccccccccccccccccccccccccccc"
	fake.props[hash] = qbt.TorrentProperties{PiecesNum: 3, Name: "Show.S01", SavePath: "/data/downloads/Show.S01"}
	fake.files[hash] = qbt.TorrentFiles{
		{Index: 0, Name: "e01.mp4", Size: 100, Priority: 1}, // already selected from a prior file pick
		{Index: 1, Name: "e02.mp4", Size: 100, Priority: 0},
	}
	fake.torrents[hash] = qbt.Torrent{Hash: hash, Category: "tsa-download"}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := m.AddTorrent(ctx, "magnet:?xt=urn:btih:"+hash)
	if err != nil {
		t.Fatalf("AddTorrent: %v", err)
	}
	if info.Hash != hash {
		t.Errorf("unexpected info: %+v", info)
	}

	if len(fake.added) != 0 {
		t.Errorf("expected no add-torrent call for an already-tracked hash, got %d", len(fake.added))
	}
	if calls := fake.getPriorityCalls(); len(calls) != 0 {
		t.Errorf("expected no priority reset for an already-tracked hash, got %v", calls)
	}
	if !info.Files[0].Selected {
		t.Errorf("expected already-selected file to stay selected: %+v", info.Files[0])
	}
}

func TestDownloadManager_AddTorrent_MetadataTimeout(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)
	// No props/files ever populated for this hash — metadata never arrives.

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := m.AddTorrent(ctx, "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if !errors.Is(err, ErrDownloadMetadataTimeout) {
		t.Fatalf("expected ErrDownloadMetadataTimeout, got %v", err)
	}
}

func TestDownloadManager_AddTorrent_InvalidMagnet(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)
	_, err := m.AddTorrent(context.Background(), "magnet:?dn=no-btih")
	if !errors.Is(err, ErrDownloadInvalidMagnet) {
		t.Fatalf("expected ErrDownloadInvalidMagnet, got %v", err)
	}
}

// TestDownloadManager_SelectFiles_Additive covers §6.2 Assumption #7: unlike
// the streaming engine's exclusive PrioritizeFile, selecting files here must
// never demote ones already selected — it only ever promotes.
func TestDownloadManager_SelectFiles_Additive(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)

	if err := m.SelectFiles(context.Background(), "hash1", []int{0, 2}); err != nil {
		t.Fatalf("SelectFiles: %v", err)
	}

	calls := fake.getPriorityCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 priority call, got %d: %v", len(calls), calls)
	}
	if calls[0].priority != 1 {
		t.Errorf("expected priority=1 (promote), got %d", calls[0].priority)
	}
	if calls[0].ids != "0|2" {
		t.Errorf("expected ids %q, got %q", "0|2", calls[0].ids)
	}
}

func TestDownloadManager_List_FiltersByCategory(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)
	fake.torrents["a"] = qbt.Torrent{Hash: "a", Name: "Mine", Category: "tsa-download", Progress: 0.5, DlSpeed: 100, Size: 1000}
	fake.torrents["b"] = qbt.Torrent{Hash: "b", Name: "NotMine", Category: "other"}

	list, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Hash != "a" {
		t.Fatalf("expected only the tsa-download torrent, got %+v", list)
	}
	if list[0].Progress != 0.5 || list[0].DlSpeed != 100 || list[0].Size != 1000 {
		t.Errorf("unexpected fields: %+v", list[0])
	}
}

func TestDownloadManager_Get_NotFound(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)
	_, err := m.Get(context.Background(), "missing")
	if !errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("expected ErrDownloadNotFound, got %v", err)
	}
}

func TestDownloadManager_Get_IncludesFileSelection(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)
	fake.torrents["a"] = qbt.Torrent{Hash: "a", Name: "Movie", Category: "tsa-download"}
	fake.files["a"] = qbt.TorrentFiles{
		{Index: 0, Name: "a.mp4", Size: 100, Progress: 1, Priority: 1},
		{Index: 1, Name: "b.mp4", Size: 200, Progress: 0, Priority: 0},
	}

	info, err := m.Get(context.Background(), "a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(info.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(info.Files))
	}
	if !info.Files[0].Selected || info.Files[0].Downloaded != 100 {
		t.Errorf("file 0 = %+v, want selected with downloaded=100", info.Files[0])
	}
	if info.Files[1].Selected || info.Files[1].Downloaded != 0 {
		t.Errorf("file 1 = %+v, want not selected with downloaded=0", info.Files[1])
	}
}

// TestDownloadManager_Get_DegradesGracefullyOnFilesError guards against Get
// discarding torrent-level info (progress/state/etc., already fetched
// successfully) just because the follow-up file-list call had a transient
// failure — the polled detail endpoint should keep working, minus file
// detail, rather than erroring outright.
func TestDownloadManager_Get_DegradesGracefullyOnFilesError(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)
	fake.torrents["a"] = qbt.Torrent{Hash: "a", Name: "Movie", Category: "tsa-download", Progress: 0.75}
	fake.filesErr = errors.New("qbittorrent unreachable")

	info, err := m.Get(context.Background(), "a")
	if err != nil {
		t.Fatalf("Get should degrade gracefully, not fail: %v", err)
	}
	if info.Hash != "a" || info.Progress != 0.75 {
		t.Errorf("expected torrent-level info to survive a files-fetch failure, got %+v", info)
	}
	if info.Files != nil {
		t.Errorf("expected nil Files on a files-fetch failure, got %v", info.Files)
	}
}

func TestDownloadManager_Delete_AlwaysDeletesFiles(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManager(t, fake)
	if err := m.Delete(context.Background(), "hash1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(fake.deleteCalls) != 1 || len(fake.deleteCalls[0]) != 1 || fake.deleteCalls[0][0] != "hash1" {
		t.Fatalf("unexpected delete calls: %v", fake.deleteCalls)
	}
}
