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

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
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
	s1, err := mgr.AddSession(context.Background(), magnet, "")
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	s2, err := mgr.AddSession(context.Background(), magnet, "")
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

	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", ""); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:BBB", "")
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
	_, err := mgr.AddSession(ctx, "magnet:?xt=urn:btih:SLOW", "")
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
	if _, err := mgr.AddSession(context.Background(), "http://not-a-magnet", ""); !errors.Is(err, ErrInvalidMagnet) {
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

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
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

	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", ""); err != nil {
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

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
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

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
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

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
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
	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
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

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	tor.triggerGone()

	if _, ok := mgr.Get(s.ID); ok {
		t.Error("session should be removed from bookkeeping after the gone-callback fires")
	}
}

// --- docs/STREAMING.md §7: pause/resume/retain lifecycle + clientLister ---

func readyQBTorrent(name string, files ...TorrentFile) func(string) *fakeQBTorrent {
	return func(string) *fakeQBTorrent {
		return &fakeQBTorrent{name: name, gotInfo: closedChan(), files: files}
	}
}

func testQBConfig(t *testing.T) Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.QBitPauseTimeout = time.Minute
	cfg.QBitRetentionTimeout = 24 * time.Hour
	return cfg
}

func TestCollectIdle_PausesInsteadOfRemoving(t *testing.T) {
	cfg := testQBConfig(t)
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle()

	mgr.mu.Lock()
	n := len(mgr.sessions)
	paused := s.paused
	mgr.mu.Unlock()
	if n != 1 {
		t.Fatalf("paused session should stay tracked, got %d sessions", n)
	}
	if !paused {
		t.Error("session should be marked paused")
	}
	if tor := client.byIH["AAA"]; tor == nil || tor.wasDropped() {
		t.Error("paused torrent should not be dropped")
	}
	if tor := client.byIH["AAA"]; tor == nil || !tor.isPaused() {
		t.Error("underlying torrent should have Pause() called")
	}
}

func TestCollectIdle_RemovesAfterRetentionTimeout(t *testing.T) {
	cfg := testQBConfig(t)
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", ""); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// First tick: idle past pause timeout → paused.
	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle()
	mgr.mu.Lock()
	n := len(mgr.sessions)
	mgr.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected session paused (still tracked) after first tick, got %d", n)
	}

	// Second tick, still within retention: must stay paused, not removed.
	mgr.SetClock(func() time.Time { return base.Add(2*time.Minute + time.Hour) })
	mgr.collectIdle()
	mgr.mu.Lock()
	n = len(mgr.sessions)
	mgr.mu.Unlock()
	if n != 1 {
		t.Fatalf("paused session should survive within the retention window, got %d", n)
	}

	// Third tick, past retention (measured from when it was paused): removed.
	mgr.SetClock(func() time.Time { return base.Add(2*time.Minute + 25*time.Hour) })
	mgr.collectIdle()
	mgr.mu.Lock()
	n = len(mgr.sessions)
	mgr.mu.Unlock()
	if n != 0 {
		t.Errorf("paused session past retention should finally be removed, got %d", n)
	}
	if tor := client.byIH["AAA"]; tor == nil || !tor.wasDropped() {
		t.Error("session past retention should be dropped")
	}
}

func TestCollectIdle_PauseFailureRetriedNextTick(t *testing.T) {
	cfg := testQBConfig(t)
	client := newFakeQBClient(func(string) *fakeQBTorrent {
		return &fakeQBTorrent{name: "M", gotInfo: closedChan(), pauseErr: errors.New("qbittorrent unreachable")}
	})
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle()

	mgr.mu.Lock()
	paused := s.paused
	n := len(mgr.sessions)
	mgr.mu.Unlock()
	if paused {
		t.Error("a failed Pause() must not be recorded as paused")
	}
	if n != 1 {
		t.Fatalf("session should remain tracked (active) after a failed pause, got %d", n)
	}

	// Fix the fake and let the next tick retry successfully.
	client.byIH["AAA"].mu.Lock()
	client.byIH["AAA"].pauseErr = nil
	client.byIH["AAA"].mu.Unlock()
	mgr.SetClock(func() time.Time { return base.Add(4 * time.Minute) })
	mgr.collectIdle()

	mgr.mu.Lock()
	paused = s.paused
	mgr.mu.Unlock()
	if !paused {
		t.Error("pause should succeed and be recorded once the underlying failure clears")
	}
}

func TestResumeIfPaused_ErrorPropagatesAndSessionStaysPaused(t *testing.T) {
	cfg := testQBConfig(t)
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	const magnet = "magnet:?xt=urn:btih:AAA"
	s, err := mgr.AddSession(context.Background(), magnet, "")
	if err != nil {
		t.Fatalf("first add: %v", err)
	}

	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle()
	mgr.mu.Lock()
	if !s.paused {
		mgr.mu.Unlock()
		t.Fatal("session should be paused after idle")
	}
	mgr.mu.Unlock()

	client.byIH["AAA"].mu.Lock()
	client.byIH["AAA"].resumeErr = errors.New("qbittorrent unreachable")
	client.byIH["AAA"].mu.Unlock()

	if _, err := mgr.AddSession(context.Background(), magnet, ""); err == nil {
		t.Fatal("expected AddSession to surface the Resume() failure, got nil error")
	}
	mgr.mu.Lock()
	stillPaused := s.paused
	mgr.mu.Unlock()
	if !stillPaused {
		t.Error("session must remain marked paused when Resume() fails")
	}

	// OpenFile's direct-reconnect resume path must surface the same failure.
	if _, _, err := mgr.OpenFile(s.ID, 0); err == nil {
		t.Fatal("expected OpenFile to surface the Resume() failure, got nil error")
	}
}

func TestAddSession_ReuseResumesPausedSession(t *testing.T) {
	cfg := testQBConfig(t)
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	const magnet = "magnet:?xt=urn:btih:AAA"
	s, err := mgr.AddSession(context.Background(), magnet, "")
	if err != nil {
		t.Fatalf("first add: %v", err)
	}

	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle()
	mgr.mu.Lock()
	if !s.paused {
		mgr.mu.Unlock()
		t.Fatal("session should be paused after idle")
	}
	mgr.mu.Unlock()

	s2, err := mgr.AddSession(context.Background(), magnet, "")
	if err != nil {
		t.Fatalf("reuse add: %v", err)
	}
	if s2.ID != s.ID {
		t.Fatalf("expected the same session reused, got %s vs %s", s.ID, s2.ID)
	}
	mgr.mu.Lock()
	stillPaused := s.paused
	mgr.mu.Unlock()
	if stillPaused {
		t.Error("reusing a paused session should resume it")
	}
	if tor := client.byIH["AAA"]; tor == nil || tor.isPaused() {
		t.Error("underlying torrent should have Resume() called")
	}
}

func TestOpenFile_ResumesPausedSession(t *testing.T) {
	cfg := testQBConfig(t)
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("0123456789")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle()

	r, _, err := mgr.OpenFile(s.ID, 0)
	if err != nil {
		t.Fatalf("OpenFile on paused session: %v", err)
	}
	r.Close()

	mgr.mu.Lock()
	paused := s.paused
	mgr.mu.Unlock()
	if paused {
		t.Error("a direct reconnect should resume a paused session")
	}
}

func TestAddSession_PausedSessionExcludedFromCapacity(t *testing.T) {
	cfg := testQBConfig(t)
	cfg.MaxActive = 1
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(cfg, client)
	defer mgr.Close()

	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time { return base })

	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", ""); err != nil {
		t.Fatalf("first add: %v", err)
	}
	mgr.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	mgr.collectIdle() // pauses AAA

	// A second, different torrent should now fit despite MaxActive=1, since
	// the paused session doesn't count (docs/STREAMING.md §7 Decision #29).
	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:BBB", ""); err != nil {
		t.Fatalf("second add should succeed with a paused slot free: %v", err)
	}
}

func TestAddSession_TagsClientID(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(testQBConfig(t), client)
	defer mgr.Close()

	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "browser-1"); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	tags := client.byIH["AAA"].tags()
	if len(tags) != 1 || tags[0] != "browser-1" {
		t.Errorf("expected clientID tagged, got %v", tags)
	}
}

func TestManagerClientLister_DelegatesAndSyncsBookkeeping(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(testQBConfig(t), client)
	defer mgr.Close()

	client.list = []TorrentSummary{{Hash: "AAA", Name: "M"}}
	list, err := mgr.ListTorrents(context.Background(), "browser-1")
	if err != nil || len(list) != 1 || list[0].Hash != "AAA" {
		t.Fatalf("ListTorrents: got %v, %v", list, err)
	}

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "browser-1")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	if err := mgr.ResumeTorrentByHash(context.Background(), "AAA", "browser-1"); err != nil {
		t.Fatalf("ResumeTorrentByHash: %v", err)
	}
	if len(client.resumeCalls) != 1 || client.resumeCalls[0] != "AAA" {
		t.Errorf("expected client-level resume call, got %v", client.resumeCalls)
	}

	if err := mgr.MoveToDownloads(context.Background(), "AAA", "browser-1", "tsa-download"); err != nil {
		t.Fatalf("MoveToDownloads: %v", err)
	}
	if len(client.moveCalls) != 1 || client.moveCalls[0].category != "tsa-download" {
		t.Errorf("expected move-to-category call, got %v", client.moveCalls)
	}
	if _, ok := mgr.Get(s.ID); ok {
		t.Error("session should no longer be tracked after MoveToDownloads")
	}
	if tor := client.byIH["AAA"]; tor == nil || tor.wasDropped() {
		t.Error("MoveToDownloads must not delete the underlying data")
	}
}

func TestManagerClientLister_DeleteAndFlushSyncBookkeeping(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	mgr := NewManager(testQBConfig(t), client)
	defer mgr.Close()

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "browser-1")
	if err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := mgr.DeleteTorrentByHash(context.Background(), "AAA", "browser-1"); err != nil {
		t.Fatalf("DeleteTorrentByHash: %v", err)
	}
	if _, ok := mgr.Get(s.ID); ok {
		t.Error("session should no longer be tracked after DeleteTorrentByHash")
	}

	s2, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:BBB", "browser-1")
	if err != nil {
		t.Fatalf("AddSession BBB: %v", err)
	}
	client.flushRemoved = []string{"BBB"}
	if err := mgr.Flush(context.Background(), "browser-1"); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if _, ok := mgr.Get(s2.ID); ok {
		t.Error("session should no longer be tracked after Flush")
	}
}

func TestManagerClientLister_NotSupportedOnAnacrolix(t *testing.T) {
	// The plain fakeClient/fakeTorrent doubles don't implement clientLister,
	// mirroring the real anacrolix adapter — docs/STREAMING.md §7's
	// qBittorrent-only scope.
	mgr := NewManager(testConfig(t), newFakeClient(readyTorrent("M")))
	defer mgr.Close()

	if _, err := mgr.ListTorrents(context.Background(), ""); !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
	if err := mgr.ResumeTorrentByHash(context.Background(), "AAA", ""); !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
	if err := mgr.DeleteTorrentByHash(context.Background(), "AAA", ""); !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
	if err := mgr.MoveToDownloads(context.Background(), "AAA", "", "tsa-download"); !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
	if err := mgr.Flush(context.Background(), ""); !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
}

func TestRemove(t *testing.T) {
	mgr := NewManager(testConfig(t), newFakeClient(readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")})))
	defer mgr.Close()

	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
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
