package stream

import (
	"bytes"
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
