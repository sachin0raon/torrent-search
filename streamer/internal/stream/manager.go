package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// readahead is how far ahead of the current read position anacrolix will
// prefetch, bounding per-stream disk/RAM use while keeping playback smooth.
const readahead = 16 << 20 // 16 MiB

// Sentinel errors returned by the Manager, mapped to HTTP status codes by the
// handlers.
var (
	ErrInvalidMagnet   = errors.New("invalid magnet")
	ErrAtCapacity      = errors.New("too many active streams")
	ErrMetadataTimeout = errors.New("timed out fetching torrent metadata")
	ErrNotFound        = errors.New("session not found")
	ErrFileIndex       = errors.New("file index out of range")
)

// FileInfo describes one file within a torrent for the API/UI.
type FileInfo struct {
	Index      int    `json:"index"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Streamable bool   `json:"streamable"`
}

// FileProgress carries live download progress for one file.
type FileProgress struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Downloaded int64  `json:"downloaded"`
}

// SessionStats is the response body for GET /stream-api/sessions/{id}/stats.
type SessionStats struct {
	SessionID string         `json:"sessionId"`
	Name      string         `json:"name"`
	Seeders   int            `json:"seeders"`
	Files     []FileProgress `json:"files"`
}

// Session is one active torrent.
type Session struct {
	ID       string
	Infohash string
	Name     string

	t        Torrent
	lastRead atomic.Int64 // unix nanoseconds; touched on every stream read

	// ready and files are guarded by the owning Manager's mu.
	ready bool
	files []FileInfo
}

func (s *Session) touch(now time.Time) { s.lastRead.Store(now.UnixNano()) }

// TrackerProvider supplies extra trackers as a BEP-12 tiered announce list.
type TrackerProvider interface {
	Tiers() [][]string
}

// Manager owns the torrent client and the set of active sessions, enforces the
// concurrency cap, and runs idle garbage collection.
type Manager struct {
	cfg      Config
	client   TorrentClient
	trackers TrackerProvider  // optional; nil means no extra trackers
	now      func() time.Time // injectable clock for tests

	mu         sync.Mutex
	sessions   map[string]*Session
	byInfohash map[string]string // infohash -> sessionID

	stopGC chan struct{}
	wg     sync.WaitGroup
}

// NewManager builds a Manager. The clock defaults to time.Now; tests may
// override it via SetClock.
func NewManager(cfg Config, client TorrentClient) *Manager {
	return &Manager{
		cfg:        cfg,
		client:     client,
		now:        time.Now,
		sessions:   make(map[string]*Session),
		byInfohash: make(map[string]string),
		stopGC:     make(chan struct{}),
	}
}

// SetClock overrides the clock used for idle timing (tests only).
func (m *Manager) SetClock(now func() time.Time) { m.now = now }

// SetTrackerProvider attaches a source of extra trackers, applied to each new
// torrent to widen peer discovery.
func (m *Manager) SetTrackerProvider(p TrackerProvider) { m.trackers = p }

// WipeDownloadDir clears the ephemeral data root, giving a clean slate after a
// restart. It removes the directory's *contents* rather than the directory
// itself, since the data root is typically a mount point that cannot be
// unlinked.
func (m *Manager) WipeDownloadDir() error {
	if err := os.MkdirAll(m.cfg.DownloadDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.cfg.DownloadDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(m.cfg.DownloadDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// AddSession adds a magnet (or returns the existing session for its infohash),
// waits for metadata under ctx, and returns the ready session. The caller is
// responsible for giving ctx the metadata timeout.
func (m *Manager) AddSession(ctx context.Context, magnet string) (*Session, error) {
	if len(magnet) < 8 || magnet[:8] != "magnet:?" {
		return nil, ErrInvalidMagnet
	}

	m.mu.Lock()
	t, err := m.client.AddMagnet(magnet)
	if err != nil {
		m.mu.Unlock()
		return nil, ErrInvalidMagnet
	}
	ih := t.InfoHash()

	if id, ok := m.byInfohash[ih]; ok {
		s := m.sessions[id]
		m.mu.Unlock()
		s.touch(m.now())
		log.Printf("streamer: reusing session %s name=%q", id, s.Name)
		if err := m.awaitInfo(ctx, s); err != nil {
			return nil, err
		}
		return s, nil
	}

	if len(m.sessions) >= m.cfg.MaxActive {
		log.Printf("streamer: at capacity (%d/%d), rejected magnet", len(m.sessions), m.cfg.MaxActive)
		t.Drop()
		m.mu.Unlock()
		return nil, ErrAtCapacity
	}

	s := &Session{ID: randomID(), Infohash: ih, Name: t.Name(), t: t}
	s.touch(m.now())
	m.sessions[s.ID] = s
	m.byInfohash[ih] = s.ID
	m.mu.Unlock()

	// Widen peer discovery before waiting for metadata, so the extra trackers
	// help fetch the info dict too.
	m.addTrackers(s)

	if err := m.awaitInfo(ctx, s); err != nil {
		// Keep the session alive so a retry immediately reuses this torrent,
		// which has already built up DHT peers and started receiving inbound
		// connections. The idle GC will evict it if nobody retries.
		return nil, err
	}
	log.Printf("streamer: session %s ready name=%q files=%d", s.ID, s.Name, len(s.files))
	return s, nil
}

// addTrackers merges the provider's trackers into a new torrent, if configured.
func (m *Manager) addTrackers(s *Session) {
	if m.trackers == nil {
		return
	}
	if tiers := m.trackers.Tiers(); len(tiers) > 0 {
		s.t.AddTrackers(tiers)
	}
}

// awaitInfo blocks until the session's metadata is available (populating its
// file list) or ctx is done.
func (m *Manager) awaitInfo(ctx context.Context, s *Session) error {
	m.mu.Lock()
	ready := s.ready
	m.mu.Unlock()
	if ready {
		return nil
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.t.GotInfo():
			m.populateFiles(s)
			return nil
		case <-ticker.C:
			st := s.t.Stats()
			log.Printf("streamer: awaiting metadata session=%s total_peers=%d active_peers=%d seeders=%d",
				s.ID, st.TotalPeers, st.ActivePeers, st.ConnectedSeeders)
		case <-ctx.Done():
			st := s.t.Stats()
			log.Printf("streamer: metadata timeout session=%s total_peers=%d active_peers=%d seeders=%d (torrent kept alive for retry)",
				s.ID, st.TotalPeers, st.ActivePeers, st.ConnectedSeeders)
			return ErrMetadataTimeout
		}
	}
}

func (m *Manager) populateFiles(s *Session) {
	files := s.t.Files()
	infos := make([]FileInfo, 0, len(files))
	for i, f := range files {
		infos = append(infos, FileInfo{
			Index:      i,
			Path:       f.Path(),
			Size:       f.Length(),
			Streamable: isStreamable(f.Path()),
		})
	}
	m.mu.Lock()
	s.files = infos
	s.ready = true
	if s.Name == "" {
		s.Name = s.t.Name()
	}
	m.mu.Unlock()
}

// Get returns a session by id, touching it so an actively-browsed session is
// not garbage collected mid-use.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if ok {
		s.touch(m.now())
	}
	return s, ok
}

// Files returns the ready file list for a session.
func (m *Manager) Files(s *Session) []FileInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return s.files
}

// Ready reports whether a session's metadata has arrived.
func (m *Manager) Ready(s *Session) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return s.ready
}

// OpenFile returns a fresh reader for one file in a session. Reads through the
// returned reader keep touching the session's idle timer, so a long in-flight
// stream is not garbage collected mid-playback. The caller must Close it.
func (m *Manager) OpenFile(id string, index int) (Reader, FileInfo, error) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil, FileInfo{}, ErrNotFound
	}
	files := s.t.Files()
	if index < 0 || index >= len(files) {
		m.mu.Unlock()
		return nil, FileInfo{}, ErrFileIndex
	}
	f := files[index]
	var info FileInfo
	if index < len(s.files) {
		info = s.files[index]
	} else {
		info = FileInfo{Index: index, Path: f.Path(), Size: f.Length(), Streamable: isStreamable(f.Path())}
	}
	m.mu.Unlock()

	s.touch(m.now())
	r := f.NewReader()
	r.SetReadahead(readahead)
	now := m.now
	return &touchReader{Reader: r, onRead: func() { s.touch(now()) }}, info, nil
}

// touchReader wraps a Reader and pings onRead whenever bytes are read, so an
// active stream keeps its session's idle timer fresh for its whole duration.
type touchReader struct {
	Reader
	onRead func()
}

func (t *touchReader) Read(p []byte) (int, error) {
	n, err := t.Reader.Read(p)
	if n > 0 {
		t.onRead()
	}
	return n, err
}

// Remove stops and deletes a session by id (explicit stop).
func (m *Manager) Remove(id string) bool {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	log.Printf("streamer: session %s removed explicitly name=%q", s.ID, s.Name)
	m.remove(s)
	return true
}

// remove drops the torrent, unmaps the session, and deletes its on-disk data.
func (m *Manager) remove(s *Session) {
	m.mu.Lock()
	delete(m.sessions, s.ID)
	delete(m.byInfohash, s.Infohash)
	m.mu.Unlock()

	s.t.Drop()
	// anacrolix lays a torrent's data out under DataDir keyed by its name (a
	// directory for multi-file torrents, the file itself for single-file).
	// Remove both the name- and infohash-keyed paths to be safe.
	if s.Name != "" {
		_ = os.RemoveAll(filepath.Join(m.cfg.DownloadDir, s.Name))
	}
	_ = os.RemoveAll(filepath.Join(m.cfg.DownloadDir, s.Infohash))
}

// StartGC launches the background idle-collection loop.
func (m *Manager) StartGC() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.cfg.GCInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopGC:
				return
			case <-ticker.C:
				m.collectIdle()
			}
		}
	}()
}

// collectIdle drops every session idle longer than the configured timeout.
func (m *Manager) collectIdle() {
	now := m.now()
	cutoff := now.Add(-m.cfg.IdleTimeout).UnixNano()

	m.mu.Lock()
	var stale []*Session
	for _, s := range m.sessions {
		if s.lastRead.Load() < cutoff {
			stale = append(stale, s)
		}
	}
	m.mu.Unlock()

	for _, s := range stale {
		log.Printf("streamer: session %s idle-expired name=%q", s.ID, s.Name)
		m.remove(s)
	}
}

// GetStats returns live download stats for a session. The anacrolix calls
// (Stats, BytesCompleted) are thread-safe and intentionally made outside m.mu.
func (m *Manager) GetStats(id string) (SessionStats, bool) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return SessionStats{}, false
	}
	rawFiles := s.t.Files()
	cached := make([]FileInfo, len(s.files))
	copy(cached, s.files)
	name := s.Name
	m.mu.Unlock()

	// Touch the idle timer so that polling the stats panel keeps the session
	// alive even when no file is being actively streamed.
	s.touch(m.now())

	stat := s.t.Stats()
	fps := make([]FileProgress, len(rawFiles))
	for i, f := range rawFiles {
		var size int64
		if i < len(cached) {
			size = cached[i].Size
		} else {
			size = f.Length()
		}
		fps[i] = FileProgress{
			Index:      i,
			Name:       filepath.Base(f.Path()),
			Size:       size,
			Downloaded: f.BytesCompleted(),
		}
	}
	return SessionStats{
		SessionID: id,
		Name:      name,
		Seeders:   stat.ConnectedSeeders,
		Files:     fps,
	}, true
}

// Close stops the GC loop and the torrent client.
func (m *Manager) Close() {
	close(m.stopGC)
	m.wg.Wait()
	m.client.Close()
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
