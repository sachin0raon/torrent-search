package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
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
	// ErrTorrentNotFound is returned by the clientLister-backed hash operations
	// (List/Resume/Delete/MoveToDownloads) when no matching, clientID-owned
	// torrent exists — docs/STREAMING.md §7.
	ErrTorrentNotFound = errors.New("torrent not found")
	// ErrNotSupported is returned by the clientLister-backed Manager methods
	// when the active engine doesn't implement clientLister (e.g. anacrolix) —
	// docs/STREAMING.md §7 Assumption/Constraint that this feature is
	// qBittorrent-engine-only.
	ErrNotSupported = errors.New("not supported by the active engine")
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

// TorrentSummary describes one torrent in the qBittorrent engine's category,
// for the Active Streams panel (docs/STREAMING.md §7.6) — sourced from a
// fresh qBittorrent query, not Manager's in-memory session state (§7
// Decision #32), so it deliberately doesn't carry a session id.
type TorrentSummary struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	Progress   float64 `json:"progress"`
	Size       int64   `json:"size"`
	Downloaded int64   `json:"downloaded"`
	Paused     bool    `json:"paused"`
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

	// paused and pausedAt implement the three-state lifecycle (active/paused/
	// gone) for engines implementing pausable — docs/STREAMING.md §7. Guarded
	// by the owning Manager's mu. pausedAt is unix nanoseconds, valid only
	// when paused is true.
	paused   bool
	pausedAt int64
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
	qbit     *QBitPeerSource  // optional; nil means disabled
	now      func() time.Time // injectable clock for tests

	mu             sync.Mutex
	sessions       map[string]*Session
	byInfohash     map[string]string // infohash -> sessionID
	pendingRemoval []*Session        // sessions whose Drop() failed; retried each GC tick

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

// SetQBitPeerSource attaches a qBittorrent-backed peer discovery source. When
// set, each new session triggers a background goroutine that adds the magnet to
// qBittorrent and injects discovered peers directly into the anacrolix torrent.
func (m *Manager) SetQBitPeerSource(q *QBitPeerSource) { m.qbit = q }

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
// responsible for giving ctx the metadata timeout. clientID (empty if the
// requesting browser sent none) tags the underlying torrent on engines that
// support it, so the Active Streams panel can later find it
// (docs/STREAMING.md §7).
func (m *Manager) AddSession(ctx context.Context, magnet, clientID string) (*Session, error) {
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
		if err := m.resumeIfPaused(s); err != nil {
			log.Printf("streamer: session %s resume-on-reuse failed: %v", s.ID, err)
			return nil, err
		}
		// Additive tagging (mirrors download_manager.go's AddTorrent): a second
		// browser reusing a session another browser started still gains
		// visibility into it via its own clientID.
		if tg, ok := s.t.(clientIDTaggable); ok {
			tg.TagClientID(clientID)
		}
		if err := m.awaitInfo(ctx, s); err != nil {
			return nil, err
		}
		return s, nil
	}

	if m.activeCountLocked() >= m.cfg.MaxActive {
		log.Printf("streamer: at capacity (%d/%d), rejected magnet", m.activeCountLocked(), m.cfg.MaxActive)
		if err := t.Drop(); err != nil {
			// This torrent was never tracked in a Session, so there's nothing to
			// retry against; best-effort only (matches the untracked nature of a
			// rejected add).
			log.Printf("streamer: drop of rejected magnet failed: %v", err)
		}
		m.mu.Unlock()
		return nil, ErrAtCapacity
	}

	s := &Session{ID: randomID(), Infohash: ih, Name: t.Name(), t: t}
	s.touch(m.now())
	m.sessions[s.ID] = s
	m.byInfohash[ih] = s.ID
	m.mu.Unlock()

	if tg, ok := s.t.(clientIDTaggable); ok {
		tg.TagClientID(clientID)
	}

	// Engines that can detect their underlying torrent was deleted out-of-band
	// (e.g. the qBittorrent engine, directly via qBittorrent's own UI, not
	// through this app) implement this optional interface so Manager's own
	// bookkeeping gets cleaned up immediately — otherwise a subsequent request
	// for this session would repeat the same detect-then-fail cycle instead of
	// getting the existing clean 410 Gone / "Restart stream" flow.
	if gn, ok := s.t.(goneNotifiable); ok {
		gn.SetGoneCallback(func() { m.remove(s) })
	}

	// Widen peer discovery before waiting for metadata, so the extra trackers
	// help fetch the info dict too.
	m.addTrackers(s)
	go m.proactiveAnnounce(s)
	if m.qbit != nil {
		// Tracked via m.wg so Close() waits for it — otherwise an in-flight
		// InjectPeers call (up to qbtMaxDuration) could outlive process shutdown,
		// which matters once a qBittorrent-engine deployment exists: an overlapping
		// old/new container during a redeploy is exactly the scenario the category
		// guard in QBitPeerSource.InjectPeers protects against.
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.qbit.InjectPeers(magnet, ih, s.t.GotInfo(), m.makePeerAdder(s))
		}()
	}

	if err := m.awaitInfo(ctx, s); err != nil {
		// Keep the session alive so a retry immediately reuses this torrent,
		// which has already built up DHT peers and started receiving inbound
		// connections. The idle GC will evict it if nobody retries.
		return nil, err
	}
	log.Printf("streamer: session %s ready name=%q files=%d", s.ID, s.Name, len(s.files))
	return s, nil
}

// makePeerAdder returns a function that injects peer addresses into the session's
// underlying torrent. The type assertion is done once here so the hot path
// (called on every poll tick) avoids repeated reflection.
func (m *Manager) makePeerAdder(s *Session) func([]net.UDPAddr) {
	type peerAdder interface{ AddPeers([]net.UDPAddr) }
	ap, ok := s.t.(peerAdder)
	if !ok {
		return func([]net.UDPAddr) {}
	}
	return ap.AddPeers
}

// proactiveAnnounce fires HTTP announces to public trackers immediately after a
// session is created, then injects the returned peer addresses directly into
// the torrent. This gives anacrolix a warm peer list without waiting for the
// DHT cold-start, accelerating both metadata fetch and streaming.
func (m *Manager) proactiveAnnounce(s *Session) {
	if m.trackers == nil {
		return
	}
	tiers := m.trackers.Tiers()
	if len(tiers) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	urls := make([]string, 0, len(tiers))
	for _, tier := range tiers {
		urls = append(urls, tier...)
	}

	peers := announcePeers(ctx, s.Infohash, uint16(m.cfg.TorrentPort), urls)
	if len(peers) > 0 {
		m.makePeerAdder(s)(peers)
	}
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

	if err := m.resumeIfPaused(s); err != nil {
		return nil, FileInfo{}, fmt.Errorf("resume session: %w", err)
	}

	s.touch(m.now())
	// Engines that support downloading only the picked file (e.g. the qBittorrent
	// engine, for season-pack bandwidth efficiency) implement this optional
	// interface; anacrolix doesn't, so this is a no-op on that path — same
	// type-assertion pattern as makePeerAdder above.
	if fp, ok := s.t.(filePrioritizer); ok {
		fp.PrioritizeFile(index)
	}
	r := f.NewReader()
	r.SetReadahead(readahead)
	now := m.now
	return &touchReader{Reader: r, onRead: func() { s.touch(now()) }}, info, nil
}

// filePrioritizer is implemented by engines that can steer download priority
// toward a single file within a multi-file torrent (season packs).
type filePrioritizer interface{ PrioritizeFile(index int) }

// goneNotifiable is implemented by engines that can detect their underlying
// torrent was deleted out-of-band and want Manager to clean up bookkeeping
// for it immediately rather than waiting for a subsequent request to hit the
// same failure again.
type goneNotifiable interface{ SetGoneCallback(func()) }

// pausable is implemented by engines that can suspend a torrent without
// removing it (e.g. the qBittorrent engine). collectIdle type-asserts this to
// decide between the three-state (active/paused/gone) lifecycle and today's
// binary (active/gone) one anacrolix still uses (docs/STREAMING.md §7
// Decision #27).
type pausable interface {
	Pause() error
	Resume() error
}

// clientIDTaggable is implemented by engines that can tag a torrent with the
// requesting browser's clientID, so it can later be found by a
// clientID-scoped clientLister query (docs/STREAMING.md §7 Assumption #6).
type clientIDTaggable interface{ TagClientID(clientID string) }

// clientLister is implemented by engines that can enumerate and manage their
// own category-tagged torrents directly against the backing client (e.g. the
// qBittorrent engine), powering the Active Streams panel's qBittorrent-aware
// view (docs/STREAMING.md §7.6). anacrolix doesn't implement this — Manager's
// methods below return ErrNotSupported when it's absent.
type clientLister interface {
	ListTorrents(ctx context.Context, clientID string) ([]TorrentSummary, error)
	ResumeTorrent(ctx context.Context, hash, clientID string) error
	DeleteTorrent(ctx context.Context, hash, clientID string) error
	MoveToCategory(ctx context.Context, hash, clientID, targetCategory string) error
	FlushCategory(ctx context.Context, clientID string) ([]string, error)
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

// activeCountLocked counts non-paused sessions. Must be called with m.mu
// already held. Paused sessions don't count against MaxActive — an
// idle-but-retained torrent shouldn't block a new stream from starting
// (docs/STREAMING.md §7 Decision #29).
func (m *Manager) activeCountLocked() int {
	n := 0
	for _, s := range m.sessions {
		if !s.paused {
			n++
		}
	}
	return n
}

// resumeIfPaused resumes s if it's currently paused, clearing the paused
// state on success. Covers the two resume paths that reach a session
// directly rather than through the clientLister-backed hash operations:
// infohash-reuse (AddSession) and a direct reconnect to a still-known
// session id (OpenFile) — docs/STREAMING.md §7.5.
func (m *Manager) resumeIfPaused(s *Session) error {
	m.mu.Lock()
	paused := s.paused
	m.mu.Unlock()
	if !paused {
		return nil
	}
	p, ok := s.t.(pausable)
	if !ok {
		return nil // defensive: only pausable sessions are ever marked paused
	}
	if err := p.Resume(); err != nil {
		return fmt.Errorf("resume session: %w", err)
	}
	m.mu.Lock()
	s.paused = false
	s.pausedAt = 0
	m.mu.Unlock()
	log.Printf("streamer: session %s resumed name=%q", s.ID, s.Name)
	return nil
}

// pauseSession pauses a session's underlying torrent and marks it paused in
// Manager's bookkeeping, keeping it in the session maps (unlike remove) so
// it remains resumable — docs/STREAMING.md §7. A failed Pause() call is
// logged and left for the next GC tick to retry, rather than marking it
// paused when the underlying torrent might still be actively
// downloading/seeding.
func (m *Manager) pauseSession(s *Session) {
	p, ok := s.t.(pausable)
	if !ok {
		return
	}
	if err := p.Pause(); err != nil {
		log.Printf("streamer: session %s pause failed, will retry next tick: %v", s.ID, err)
		return
	}
	m.mu.Lock()
	s.paused = true
	s.pausedAt = m.now().UnixNano()
	m.mu.Unlock()
	log.Printf("streamer: session %s paused (idle) name=%q", s.ID, s.Name)
}

// ListTorrents returns every torrent in the qBittorrent engine's category
// scoped to clientID, for the Active Streams panel (docs/STREAMING.md §7.6).
// Returns ErrNotSupported if the active engine doesn't implement
// clientLister (i.e. anacrolix).
func (m *Manager) ListTorrents(ctx context.Context, clientID string) ([]TorrentSummary, error) {
	cl, ok := m.client.(clientLister)
	if !ok {
		return nil, ErrNotSupported
	}
	return cl.ListTorrents(ctx, clientID)
}

// ResumeTorrentByHash resumes a torrent by its qBittorrent hash — the Active
// Streams panel's explicit Resume action — and syncs Manager's own
// bookkeeping if it has a tracked session for that hash, so a subsequent
// direct reconnect doesn't attempt to resume it again (docs/STREAMING.md
// §7.5/§7.6).
func (m *Manager) ResumeTorrentByHash(ctx context.Context, hash, clientID string) error {
	cl, ok := m.client.(clientLister)
	if !ok {
		return ErrNotSupported
	}
	if err := cl.ResumeTorrent(ctx, hash, clientID); err != nil {
		return err
	}
	m.mu.Lock()
	if id, ok := m.byInfohash[hash]; ok {
		if s, ok := m.sessions[id]; ok {
			s.paused = false
			s.pausedAt = 0
		}
	}
	m.mu.Unlock()
	return nil
}

// DeleteTorrentByHash deletes a torrent by hash immediately, skipping the
// retention grace period, syncing Manager's own bookkeeping if it has a
// tracked session for that hash (docs/STREAMING.md §7.6).
func (m *Manager) DeleteTorrentByHash(ctx context.Context, hash, clientID string) error {
	cl, ok := m.client.(clientLister)
	if !ok {
		return ErrNotSupported
	}
	if err := cl.DeleteTorrent(ctx, hash, clientID); err != nil {
		return err
	}
	m.forgetByHash(hash)
	return nil
}

// MoveToDownloads recategorizes a torrent to the download-manager's category
// and drops it from Manager's own tracking, without deleting its data —
// ownership transfers to the Download Manager (docs/STREAMING.md §7 Decision
// #35).
func (m *Manager) MoveToDownloads(ctx context.Context, hash, clientID, downloadCategory string) error {
	cl, ok := m.client.(clientLister)
	if !ok {
		return ErrNotSupported
	}
	if err := cl.MoveToCategory(ctx, hash, clientID, downloadCategory); err != nil {
		return err
	}
	m.forgetByHash(hash)
	return nil
}

// Flush deletes every torrent in the qBittorrent engine's category matching
// clientID (empty clientID flushes the whole category), syncing Manager's
// own bookkeeping for any of them that were tracked (docs/STREAMING.md
// §7.6).
func (m *Manager) Flush(ctx context.Context, clientID string) error {
	cl, ok := m.client.(clientLister)
	if !ok {
		return ErrNotSupported
	}
	removed, err := cl.FlushCategory(ctx, clientID)
	if err != nil {
		return err
	}
	for _, hash := range removed {
		m.forgetByHash(hash)
	}
	return nil
}

// forgetByHash removes a session from Manager's bookkeeping by infohash
// without calling Drop() — the underlying torrent has already been deleted
// or handed off to another category by the caller, so no further cleanup
// call is needed (and for MoveToDownloads, the data must specifically NOT be
// deleted).
func (m *Manager) forgetByHash(hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byInfohash[hash]
	if !ok {
		return
	}
	delete(m.sessions, id)
	delete(m.byInfohash, hash)
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

// remove unmaps the session and drops/cleans up its underlying torrent.
func (m *Manager) remove(s *Session) {
	m.mu.Lock()
	delete(m.sessions, s.ID)
	delete(m.byInfohash, s.Infohash)
	m.mu.Unlock()

	m.dropAndCleanup(s)
}

// dropAndCleanup drops a session's torrent and deletes its on-disk data. If Drop
// fails (e.g. the qBittorrent engine's backing instance is unreachable), the
// session is queued for retry on the next GC tick instead of being silently
// abandoned — otherwise its data could stay orphaned until the next process
// restart, which for a long-lived container could be a very long time.
func (m *Manager) dropAndCleanup(s *Session) {
	if err := s.t.Drop(); err != nil {
		log.Printf("streamer: session %s drop failed, will retry: %v", s.ID, err)
		m.mu.Lock()
		m.pendingRemoval = append(m.pendingRemoval, s)
		m.mu.Unlock()
		return
	}
	// anacrolix lays a torrent's data out under DataDir keyed by its name (a
	// directory for multi-file torrents, the file itself for single-file).
	// Remove both the name- and infohash-keyed paths to be safe. For the
	// qBittorrent engine, Drop() already deleted the data via the API, so these
	// are harmless no-ops (the paths never existed under cfg.DownloadDir).
	if s.Name != "" {
		_ = os.RemoveAll(filepath.Join(m.cfg.DownloadDir, s.Name))
	}
	_ = os.RemoveAll(filepath.Join(m.cfg.DownloadDir, s.Infohash))
}

// retryPendingRemovals retries Drop() for sessions whose removal previously
// failed. Called once per GC tick, bounding retry cadence to GCInterval.
func (m *Manager) retryPendingRemovals() {
	m.mu.Lock()
	pending := m.pendingRemoval
	m.pendingRemoval = nil
	m.mu.Unlock()

	for _, s := range pending {
		m.dropAndCleanup(s) // re-appends to m.pendingRemoval on repeat failure
	}
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
				m.retryPendingRemovals()
			}
		}
	}()
}

// collectIdle runs the idle-GC sweep. Sessions on an engine implementing
// pausable (the qBittorrent engine) get a three-state lifecycle: idle past
// QBitPauseTimeout → paused, not removed; already paused past
// QBitRetentionTimeout (measured from when it was paused, uniformly for
// complete and incomplete downloads — Decision #30) → finally removed.
// Sessions on an engine without pausable (anacrolix) keep today's exact
// binary behavior: idle past IdleTimeout → removed directly
// (docs/STREAMING.md §7 Decision #27).
func (m *Manager) collectIdle() {
	now := m.now()
	idleCutoff := now.Add(-m.cfg.IdleTimeout).UnixNano()
	pauseCutoff := now.Add(-m.cfg.QBitPauseTimeout).UnixNano()
	retentionCutoff := now.Add(-m.cfg.QBitRetentionTimeout).UnixNano()

	m.mu.Lock()
	var toRemove, toPause []*Session
	for _, s := range m.sessions {
		if s.paused {
			if s.pausedAt < retentionCutoff {
				toRemove = append(toRemove, s)
			}
			continue
		}
		if _, ok := s.t.(pausable); ok {
			if s.lastRead.Load() < pauseCutoff {
				toPause = append(toPause, s)
			}
			continue
		}
		if s.lastRead.Load() < idleCutoff {
			toRemove = append(toRemove, s)
		}
	}
	m.mu.Unlock()

	for _, s := range toPause {
		m.pauseSession(s)
	}
	for _, s := range toRemove {
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
