package stream

import (
	"io"

	"github.com/anacrolix/torrent"
)

// The interfaces below abstract the subset of github.com/anacrolix/torrent that
// the Manager and handlers use, so tests can substitute a fake client with no
// network/DHT.

// Reader is a seekable, closable stream over a single file within a torrent,
// with a tunable readahead window for smooth streaming.
type Reader interface {
	io.ReadSeekCloser
	SetReadahead(int64)
}

// TorrentFile is one file inside a torrent.
type TorrentFile interface {
	Path() string
	Length() int64
	NewReader() Reader
}

// Torrent is a single added torrent.
type Torrent interface {
	// GotInfo closes when the torrent's metadata (and thus its file list) is
	// available.
	GotInfo() <-chan struct{}
	InfoHash() string
	Name() string
	Files() []TorrentFile
	// AddTrackers merges extra trackers (a BEP-12 tiered announce list) into the
	// torrent to widen peer discovery.
	AddTrackers(announceList [][]string)
	// Drop removes the torrent from its client, stopping all activity.
	Drop()
}

// TorrentClient adds magnets and can be closed on shutdown.
type TorrentClient interface {
	AddMagnet(uri string) (Torrent, error)
	Close()
}

// --- anacrolix/torrent adapter ---

type anacrolixClient struct{ c *torrent.Client }

// NewAnacrolixClient builds a real torrent client rooted at dataDir. Seeding
// and uploads are disabled; this box only leeches for playback.
func NewAnacrolixClient(dataDir string) (TorrentClient, error) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.Seed = false
	cfg.NoUpload = true
	c, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &anacrolixClient{c: c}, nil
}

func (a *anacrolixClient) AddMagnet(uri string) (Torrent, error) {
	t, err := a.c.AddMagnet(uri)
	if err != nil {
		return nil, err
	}
	return &anacrolixTorrent{t: t}, nil
}

func (a *anacrolixClient) Close() { a.c.Close() }

type anacrolixTorrent struct{ t *torrent.Torrent }

func (a *anacrolixTorrent) GotInfo() <-chan struct{}  { return a.t.GotInfo() }
func (a *anacrolixTorrent) InfoHash() string          { return a.t.InfoHash().HexString() }
func (a *anacrolixTorrent) Name() string              { return a.t.Name() }
func (a *anacrolixTorrent) AddTrackers(al [][]string) { a.t.AddTrackers(al) }
func (a *anacrolixTorrent) Drop()                     { a.t.Drop() }

func (a *anacrolixTorrent) Files() []TorrentFile {
	files := a.t.Files()
	out := make([]TorrentFile, 0, len(files))
	for _, f := range files {
		out = append(out, &anacrolixFile{f: f})
	}
	return out
}

type anacrolixFile struct{ f *torrent.File }

func (a *anacrolixFile) Path() string  { return a.f.Path() }
func (a *anacrolixFile) Length() int64 { return a.f.Length() }
func (a *anacrolixFile) NewReader() Reader {
	r := a.f.NewReader()
	// Responsive readers prioritise the region currently being read, which is
	// exactly what streaming/seeking wants.
	r.SetResponsive()
	return r
}
