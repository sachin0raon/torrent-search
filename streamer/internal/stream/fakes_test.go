package stream

import (
	"bytes"
	"context"
	"strings"
	"sync"
)

// --- test doubles for the TorrentClient/Torrent/TorrentFile/Reader interfaces ---

type fakeFile struct {
	path string
	data []byte
}

func (f *fakeFile) Path() string          { return f.path }
func (f *fakeFile) Length() int64         { return int64(len(f.data)) }
func (f *fakeFile) BytesCompleted() int64 { return int64(len(f.data)) }
func (f *fakeFile) NewReader() Reader {
	return &fakeReader{Reader: bytes.NewReader(f.data)}
}

type fakeReader struct {
	*bytes.Reader
}

func (r *fakeReader) Close() error       { return nil }
func (r *fakeReader) SetReadahead(int64) {}

type fakeTorrent struct {
	infohash string
	name     string
	gotInfo  chan struct{}
	files    []TorrentFile

	mu       sync.Mutex
	dropped  bool
	dropErr  error // if set, the next Drop() call returns this error once, then succeeds
	trackers [][]string
}

func (t *fakeTorrent) GotInfo() <-chan struct{} { return t.gotInfo }
func (t *fakeTorrent) InfoHash() string         { return t.infohash }
func (t *fakeTorrent) Name() string             { return t.name }
func (t *fakeTorrent) Files() []TorrentFile     { return t.files }
func (t *fakeTorrent) AddTrackers(al [][]string) {
	t.mu.Lock()
	t.trackers = append(t.trackers, al...)
	t.mu.Unlock()
}
func (t *fakeTorrent) addedTrackers() [][]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trackers
}
func (t *fakeTorrent) Drop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dropErr != nil {
		err := t.dropErr
		t.dropErr = nil // next Drop() call succeeds, simulating a transient failure
		return err
	}
	t.dropped = true
	return nil
}
func (t *fakeTorrent) Stats() TorrentStat { return TorrentStat{} }
func (t *fakeTorrent) wasDropped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dropped
}

type fakeClient struct {
	mu     sync.Mutex
	byIH   map[string]*fakeTorrent
	build  func(uri string) *fakeTorrent
	closed bool
}

func newFakeClient(build func(uri string) *fakeTorrent) *fakeClient {
	return &fakeClient{byIH: make(map[string]*fakeTorrent), build: build}
}

func (c *fakeClient) AddMagnet(uri string) (Torrent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ih := parseTestInfohash(uri)
	if t, ok := c.byIH[ih]; ok {
		return t, nil
	}
	t := c.build(uri)
	t.infohash = ih
	c.byIH[ih] = t
	return t, nil
}

func (c *fakeClient) Close() { c.closed = true }

// parseTestInfohash extracts the btih value from a magnet for the fake.
func parseTestInfohash(uri string) string {
	const marker = "btih:"
	i := strings.Index(uri, marker)
	if i < 0 {
		return uri
	}
	s := uri[i+len(marker):]
	if j := strings.IndexAny(s, "&"); j >= 0 {
		s = s[:j]
	}
	return s
}

// closedChan returns an already-closed channel (metadata immediately ready).
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// --- test doubles for manager.go's qBittorrent-path lifecycle
// (pausable/clientIDTaggable/clientLister) — kept deliberately separate from
// fakeTorrent/fakeClient above so anacrolix-style tests are unaffected by
// these methods existing (Go's structural interface satisfaction means
// adding Pause/Resume directly to fakeTorrent would make every existing test
// using it look "pausable" too).

type fakeQBTorrent struct {
	infohash string
	name     string
	gotInfo  chan struct{}
	files    []TorrentFile

	mu         sync.Mutex
	dropped    bool
	paused     bool
	pauseErr   error
	resumeErr  error
	taggedWith []string
}

func (t *fakeQBTorrent) GotInfo() <-chan struct{}  { return t.gotInfo }
func (t *fakeQBTorrent) InfoHash() string          { return t.infohash }
func (t *fakeQBTorrent) Name() string              { return t.name }
func (t *fakeQBTorrent) Files() []TorrentFile      { return t.files }
func (t *fakeQBTorrent) AddTrackers(al [][]string) {}
func (t *fakeQBTorrent) Stats() TorrentStat        { return TorrentStat{} }

func (t *fakeQBTorrent) Drop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dropped = true
	return nil
}

func (t *fakeQBTorrent) Pause() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pauseErr != nil {
		return t.pauseErr
	}
	t.paused = true
	return nil
}

func (t *fakeQBTorrent) Resume() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.resumeErr != nil {
		return t.resumeErr
	}
	t.paused = false
	return nil
}

func (t *fakeQBTorrent) TagClientID(clientID string) {
	if clientID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.taggedWith = append(t.taggedWith, clientID)
}

func (t *fakeQBTorrent) isPaused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.paused
}

func (t *fakeQBTorrent) wasDropped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dropped
}

func (t *fakeQBTorrent) tags() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.taggedWith))
	copy(out, t.taggedWith)
	return out
}

// moveCall records one MoveToCategory invocation on fakeQBClient.
type moveCall struct{ hash, clientID, category string }

// fakeQBClient is a TorrentClient that also implements clientLister, for
// testing Manager's qBittorrent-only methods (ListTorrents,
// ResumeTorrentByHash, DeleteTorrentByHash, MoveToDownloads, Flush) without
// depending on the real qbtClient/qbtAPI stack.
type fakeQBClient struct {
	mu    sync.Mutex
	byIH  map[string]*fakeQBTorrent
	build func(uri string) *fakeQBTorrent

	listErr      error
	list         []TorrentSummary
	resumeErr    error
	deleteErr    error
	moveErr      error
	flushErr     error
	flushRemoved []string
	resumeCalls  []string
	deleteCalls  []string
	moveCalls    []moveCall
}

func newFakeQBClient(build func(uri string) *fakeQBTorrent) *fakeQBClient {
	return &fakeQBClient{byIH: make(map[string]*fakeQBTorrent), build: build}
}

func (c *fakeQBClient) AddMagnet(uri string) (Torrent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ih := parseTestInfohash(uri)
	if t, ok := c.byIH[ih]; ok {
		return t, nil
	}
	t := c.build(uri)
	t.infohash = ih
	c.byIH[ih] = t
	return t, nil
}

func (c *fakeQBClient) Close() {}

func (c *fakeQBClient) ListTorrents(ctx context.Context, clientID string) ([]TorrentSummary, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.list, nil
}

func (c *fakeQBClient) ResumeTorrent(ctx context.Context, hash, clientID string) error {
	c.mu.Lock()
	c.resumeCalls = append(c.resumeCalls, hash)
	c.mu.Unlock()
	return c.resumeErr
}

func (c *fakeQBClient) DeleteTorrent(ctx context.Context, hash, clientID string) error {
	c.mu.Lock()
	c.deleteCalls = append(c.deleteCalls, hash)
	c.mu.Unlock()
	return c.deleteErr
}

func (c *fakeQBClient) MoveToCategory(ctx context.Context, hash, clientID, targetCategory string) error {
	c.mu.Lock()
	c.moveCalls = append(c.moveCalls, moveCall{hash, clientID, targetCategory})
	c.mu.Unlock()
	return c.moveErr
}

func (c *fakeQBClient) FlushCategory(ctx context.Context, clientID string) ([]string, error) {
	if c.flushErr != nil {
		return nil, c.flushErr
	}
	return c.flushRemoved, nil
}
