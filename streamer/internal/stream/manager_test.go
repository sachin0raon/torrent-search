package stream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		MaxActive:       5,
		IdleTimeout:     10 * time.Minute,
		MetadataTimeout: time.Second,
		DownloadDir:     t.TempDir(),
		GCInterval:      30 * time.Second,
	}
}

// readyTorrent builds a fake torrent whose metadata is immediately available.
func readyTorrent(name string, files ...TorrentFile) func(string) *fakeTorrent {
	return func(string) *fakeTorrent {
		return &fakeTorrent{name: name, gotInfo: closedChan(), files: files}
	}
}

func TestAddSession_Success(t *testing.T) {
	files := []TorrentFile{
		&fakeFile{path: "Movie/movie.mkv", data: []byte("video")},
		&fakeFile{path: "Movie/readme.txt", data: []byte("hi")},
	}
	mgr := NewManager(testConfig(t), newFakeClient(readyTorrent("Movie", files...)))
	defer mgr.Close()

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if !mgr.Ready(s) {
		t.Fatal("expected ready session")
	}
	got := mgr.Files(s)
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got))
	}
	if !got[0].Streamable {
		t.Error("mkv should be streamable")
	}
	if got[1].Streamable {
		t.Error("txt should not be streamable")
	}
}

func TestAddSession_Reuse(t *testing.T) {
	mgr := NewManager(testConfig(t), newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")})))
	defer mgr.Close()

	const magnet = "magnet:?xt=urn:btih:SAME"
	s1, err := mgr.AddSession(context.Background(), magnet)
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	s2, err := mgr.AddSession(context.Background(), magnet)
	if err != nil {
		t.Fatalf("second add: %v", err)
	}
	if s1.ID != s2.ID {
		t.Errorf("expected reused session, got %s and %s", s1.ID, s2.ID)
	}
	mgr.mu.Lock()
	n := len(mgr.sessions)
	mgr.mu.Unlock()
	if n != 1 {
		t.Errorf("expected 1 session, got %d", n)
	}
}

func TestAddSession_Capacity(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxActive = 1
	client := newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:BBB")
	if !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("expected ErrAtCapacity, got %v", err)
	}
	// The rejected torrent must have been dropped.
	if tor := client.byIH["BBB"]; tor == nil || !tor.wasDropped() {
		t.Error("over-capacity torrent should be dropped")
	}
}

func TestAddSession_MetadataTimeout(t *testing.T) {
	client := newFakeClient(func(string) *fakeTorrent {
		return &fakeTorrent{name: "M", gotInfo: make(chan struct{})} // never closes
	})
	mgr := NewManager(testConfig(t), client)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := mgr.AddSession(ctx, "magnet:?xt=urn:btih:SLOW")
	if !errors.Is(err, ErrMetadataTimeout) {
		t.Fatalf("expected ErrMetadataTimeout, got %v", err)
	}
	// The session is intentionally kept alive after a timeout so that a retry
	// can immediately reuse the torrent (which has already accumulated DHT peers
	// and tracker connections). The idle GC will evict it if nobody retries.
	mgr.mu.Lock()
	n := len(mgr.sessions)
	mgr.mu.Unlock()
	if n != 1 {
		t.Errorf("timed-out session should remain in manager for retry, got %d", n)
	}
	if tor := client.byIH["SLOW"]; tor == nil || tor.wasDropped() {
		t.Error("timed-out torrent should be kept alive for retry")
	}
}

func TestAddSession_InvalidMagnet(t *testing.T) {
	mgr := NewManager(testConfig(t), newFakeClient(readyTorrent("M")))
	defer mgr.Close()
	if _, err := mgr.AddSession(context.Background(), "http://not-a-magnet"); !errors.Is(err, ErrInvalidMagnet) {
		t.Fatalf("expected ErrInvalidMagnet, got %v", err)
	}
}

func TestCollectIdle_DropsAndDeletesData(t *testing.T) {
	cfg := testConfig(t)
	cfg.IdleTimeout = time.Minute
	client := newFakeClient(readyTorrent("Movie", &fakeFile{path: "Movie/a.mp4", data: []byte("x")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Simulate anacrolix having written the torrent's data directory.
	dataDir := filepath.Join(cfg.DownloadDir, s.Name)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Advance the clock past the idle timeout and collect.
	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle()

	mgr.mu.Lock()
	n := len(mgr.sessions)
	mgr.mu.Unlock()
	if n != 0 {
		t.Errorf("idle session should be collected, got %d", n)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("expected data dir deleted, stat err = %v", err)
	}
	if tor := client.byIH["AAA"]; tor == nil || !tor.wasDropped() {
		t.Error("idle torrent should be dropped")
	}
}

type fakeTrackers struct{ tiers [][]string }

func (f fakeTrackers) Tiers() [][]string { return f.tiers }

func TestAddSession_AppliesTrackers(t *testing.T) {
	client := newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(testConfig(t), client)
	defer mgr.Close()
	mgr.SetTrackerProvider(fakeTrackers{tiers: [][]string{{"udp://t1:80"}, {"http://t2/announce"}}})

	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA"); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	got := client.byIH["AAA"].addedTrackers()
	if len(got) != 2 || got[0][0] != "udp://t1:80" || got[1][0] != "http://t2/announce" {
		t.Errorf("expected trackers applied, got %v", got)
	}
}

func TestWipeDownloadDir_ClearsContentsNotMount(t *testing.T) {
	cfg := testConfig(t)
	mgr := NewManager(cfg, newFakeClient(readyTorrent("M")))
	defer mgr.Close()

	// Seed some stale data.
	stale := filepath.Join(cfg.DownloadDir, "old-torrent")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := mgr.WipeDownloadDir(); err != nil {
		t.Fatalf("WipeDownloadDir: %v", err)
	}
	// The root itself must still exist (it may be a mount point)...
	if _, err := os.Stat(cfg.DownloadDir); err != nil {
		t.Errorf("download dir should still exist: %v", err)
	}
	// ...but its contents are gone.
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale data should be removed, stat err = %v", err)
	}
}

func TestOpenFile_ReadKeepsSessionAlive(t *testing.T) {
	cfg := testConfig(t)
	cfg.IdleTimeout = time.Minute
	mgr := NewManager(cfg, newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("0123456789")})))
	defer mgr.Close()

	base := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	cur := base
	mgr.SetClock(func() time.Time { return cur })

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	r, _, err := mgr.OpenFile(s.ID, 0)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer r.Close()

	// Jump well past the idle timeout without a new request, then read from the
	// in-flight stream: the read must refresh lastRead so GC leaves it alone.
	cur = base.Add(10 * time.Minute)
	if _, err := r.Read(make([]byte, 4)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	mgr.collectIdle()

	mgr.mu.Lock()
	_, alive := mgr.sessions[s.ID]
	mgr.mu.Unlock()
	if !alive {
		t.Error("session with an active read must not be garbage collected")
	}
}

func TestRemove_RetriesFailedDrop(t *testing.T) {
	client := newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(testConfig(t), client)
	defer mgr.Close()

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	tor := client.byIH["AAA"]
	tor.dropErr = errors.New("qbittorrent unreachable")

	if !mgr.Remove(s.ID) {
		t.Fatal("Remove should return true even when the underlying Drop fails")
	}
	// The session must be unservable immediately, regardless of Drop's outcome.
	if _, ok := mgr.Get(s.ID); ok {
		t.Fatal("session should be gone from bookkeeping immediately")
	}
	if tor.wasDropped() {
		t.Fatal("torrent should not be marked dropped yet (Drop failed)")
	}
	mgr.mu.Lock()
	n := len(mgr.pendingRemoval)
	mgr.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected 1 pending removal, got %d", n)
	}

	// Next GC tick retries; dropErr was cleared after the first (failing) call, so
	// this attempt succeeds.
	mgr.retryPendingRemovals()
	if !tor.wasDropped() {
		t.Error("torrent should be dropped after a successful retry")
	}
	mgr.mu.Lock()
	n = len(mgr.pendingRemoval)
	mgr.mu.Unlock()
	if n != 0 {
		t.Errorf("pending removal should be cleared after success, got %d", n)
	}
}

// fakePrioritizingTorrent additionally implements filePrioritizer, simulating the
// qBittorrent engine's file-priority-on-pick behavior (anacrolix's fakeTorrent
// deliberately does not implement this interface).
type fakePrioritizingTorrent struct {
	fakeTorrent
	mu          sync.Mutex
	prioritized []int
}

func (t *fakePrioritizingTorrent) PrioritizeFile(index int) {
	t.mu.Lock()
	t.prioritized = append(t.prioritized, index)
	t.mu.Unlock()
}

type fakePrioritizingClient struct{ t *fakePrioritizingTorrent }

func (c *fakePrioritizingClient) AddMagnet(uri string) (Torrent, error) { return c.t, nil }
func (c *fakePrioritizingClient) Close()                                {}

func TestOpenFile_PrioritizesPickedFile(t *testing.T) {
	tor := &fakePrioritizingTorrent{fakeTorrent: fakeTorrent{
		infohash: "AAA",
		name:     "M",
		gotInfo:  closedChan(),
		files:    []TorrentFile{&fakeFile{path: "a.mp4", data: []byte("0123456789")}},
	}}
	mgr := NewManager(testConfig(t), &fakePrioritizingClient{t: tor})
	defer mgr.Close()

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	r, _, err := mgr.OpenFile(s.ID, 0)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer r.Close()

	tor.mu.Lock()
	got := tor.prioritized
	tor.mu.Unlock()
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("expected PrioritizeFile(0) called once, got %v", got)
	}
}

func TestOpenFile_NonPrioritizingEngineUnaffected(t *testing.T) {
	// readyTorrent's *fakeTorrent doesn't implement filePrioritizer; OpenFile must
	// simply no-op the type assertion rather than erroring or panicking.
	mgr := NewManager(testConfig(t), newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")})))
	defer mgr.Close()
	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	r, _, err := mgr.OpenFile(s.ID, 0)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	_ = r.Close()
}

// fakeGoneNotifiableTorrent additionally implements goneNotifiable, simulating
// the qBittorrent engine's out-of-band-deletion detection.
type fakeGoneNotifiableTorrent struct {
	fakeTorrent
	mu       sync.Mutex
	callback func()
}

func (t *fakeGoneNotifiableTorrent) SetGoneCallback(fn func()) {
	t.mu.Lock()
	t.callback = fn
	t.mu.Unlock()
}

func (t *fakeGoneNotifiableTorrent) triggerGone() {
	t.mu.Lock()
	fn := t.callback
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
}

type fakeGoneNotifiableClient struct{ t *fakeGoneNotifiableTorrent }

func (c *fakeGoneNotifiableClient) AddMagnet(uri string) (Torrent, error) { return c.t, nil }
func (c *fakeGoneNotifiableClient) Close()                                {}

func TestAddSession_WiresGoneCallback(t *testing.T) {
	tor := &fakeGoneNotifiableTorrent{fakeTorrent: fakeTorrent{
		infohash: "AAA", name: "M", gotInfo: closedChan(),
		files: []TorrentFile{&fakeFile{path: "a.mp4", data: []byte("x")}},
	}}
	mgr := NewManager(testConfig(t), &fakeGoneNotifiableClient{t: tor})
	defer mgr.Close()

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	tor.triggerGone()

	if _, ok := mgr.Get(s.ID); ok {
		t.Error("session should be removed from bookkeeping after the gone-callback fires")
	}
}

func TestRemove(t *testing.T) {
	mgr := NewManager(testConfig(t), newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")})))
	defer mgr.Close()

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if !mgr.Remove(s.ID) {
		t.Fatal("Remove should return true for existing session")
	}
	if mgr.Remove(s.ID) {
		t.Fatal("Remove should return false for missing session")
	}
	if _, ok := mgr.Get(s.ID); ok {
		t.Fatal("session should be gone after Remove")
	}
}
