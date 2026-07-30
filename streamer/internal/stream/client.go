package stream

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

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

// TorrentStat carries live peer metrics for one torrent.
type TorrentStat struct {
	ConnectedSeeders int
	ActivePeers      int // connections currently active (including metadata exchange)
	TotalPeers       int // all known peer addresses (connected + discovered)
}

// TorrentFile is one file inside a torrent.
type TorrentFile interface {
	Path() string
	Length() int64
	NewReader() Reader
	// BytesCompleted returns how many bytes of this file have been downloaded.
	BytesCompleted() int64
}

// Torrent is a single added torrent.
type Torrent interface {
	// GotInfo closes when the torrent's metadata (and thus its file list) is
	// available.
	GotInfo() <-chan struct{}
	InfoHash() string
	Name() string
	Files() []TorrentFile
	// Stats returns live peer and download metrics for this torrent.
	Stats() TorrentStat
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

type anacrolixClient struct {
	c       *torrent.Client
	stopDHT func() // stops the DHT state saver goroutine and flushes a final save
}

// NewAnacrolixClient builds a real torrent client rooted at dataDir. listenPort
// must be reachable from the internet (firewall + Docker port mapping) so peers
// can initiate connections and push torrent metadata. dhtStateFile is the path
// where the DHT routing table is persisted between restarts (empty = disabled).
// Seeding and uploads are disabled; this box only leeches for playback.
func NewAnacrolixClient(dataDir string, listenPort int, dhtStateFile string) (TorrentClient, error) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.Seed = false
	cfg.NoUpload = true
	cfg.ListenPort = listenPort
	// On a server with an open listen port, more simultaneous outbound attempts
	// means faster time-to-first-connection when most peers are behind NAT.
	cfg.HalfOpenConnsPerTorrent = 100
	cfg.TotalHalfOpenConns = 500     // global cap shared across all torrents; default is 100
	cfg.EstablishedConnsPerTorrent = 200 // default is 50; more connections = faster metadata
	// uTP (UDP-based transport) bypasses VPS-level TCP throttling of BitTorrent
	// traffic. UDP is confirmed unblocked. Risk: drops TCP-only peers, but
	// virtually all modern clients support uTP. Revert if connections get worse.
	cfg.DisableTCP = true
	// Prefer MSE/PE header obfuscation so the BitTorrent handshake is not
	// detectable by plain-text DPI. Non-obfuscated peers are still accepted.
	cfg.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{
		Preferred:        true,
		RequirePreferred: false,
	}
	if dhtStateFile != "" {
		cfg.DhtStartingNodes = dhtStartingNodes(dhtStateFile)
	}
	c, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	a := &anacrolixClient{c: c}
	if dhtStateFile != "" {
		a.stopDHT = startDHTStateSaver(c, dhtStateFile, 5*time.Minute)
	}
	return a, nil
}

func (a *anacrolixClient) AddMagnet(uri string) (t Torrent, err error) {
	// anacrolix/torrent calls panicif.Err when it encounters a tracker URL with
	// an unsupported scheme (e.g. wss://, i2p://). Catch that here so the panic
	// cannot escape and leave the Manager mutex permanently locked.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("torrent library panicked: %v", r)
		}
	}()
	torrent, addErr := a.c.AddMagnet(sanitizeMagnet(uri))
	if addErr != nil {
		return nil, addErr
	}
	return &anacrolixTorrent{t: torrent}, nil
}

// sanitizeMagnet normalises tracker URLs in a magnet link so that
// anacrolix/torrent does not panic. Two classes of bad input are handled:
//
//  1. The "tracker:" prefix — some sites (e.g. t-ru.org) emit tracker
//     announce URLs as "tracker:http://…" or "tracker:udp://…". The library
//     treats the scheme as "tracker", hits an unsupported-scheme branch, and
//     calls panicif.Err. We strip the prefix so the inner URL is used directly.
//
//  2. Genuinely unsupported schemes (wss://, i2p://, …) — dropped entirely.
//
// The mutex-leak danger: AddSession holds m.mu while calling AddMagnet. A
// panic there bypasses every explicit Unlock and leaves the mutex permanently
// locked. We also recover() in AddMagnet itself, but sanitizing first is the
// cleaner fix.
func sanitizeMagnet(raw string) string {
	qIdx := strings.Index(raw, "?")
	if qIdx < 0 {
		return raw
	}
	q, err := url.ParseQuery(raw[qIdx+1:])
	if err != nil {
		return raw
	}
	trs := q["tr"]
	if len(trs) == 0 {
		return raw
	}
	filtered := make([]string, 0, len(trs))
	changed := false
	for _, tr := range trs {
		// Unwrap the non-standard "tracker:" prefix before checking the scheme.
		inner := tr
		if strings.HasPrefix(strings.ToLower(tr), "tracker:") {
			inner = tr[len("tracker:"):]
			changed = true
		}
		tu, err := url.Parse(inner)
		if err != nil {
			changed = true
			continue
		}
		switch tu.Scheme {
		case "http", "https", "udp":
			filtered = append(filtered, inner)
		default:
			changed = true
		}
	}
	if !changed {
		return raw
	}
	q["tr"] = filtered
	return raw[:qIdx+1] + q.Encode()
}

func (a *anacrolixClient) Close() {
	if a.stopDHT != nil {
		a.stopDHT()
	}
	a.c.Close()
}

type anacrolixTorrent struct{ t *torrent.Torrent }

func (a *anacrolixTorrent) GotInfo() <-chan struct{}  { return a.t.GotInfo() }
func (a *anacrolixTorrent) InfoHash() string          { return a.t.InfoHash().HexString() }
func (a *anacrolixTorrent) Name() string              { return a.t.Name() }
func (a *anacrolixTorrent) AddTrackers(al [][]string) { a.t.AddTrackers(al) }
func (a *anacrolixTorrent) Drop()                     { a.t.Drop() }
func (a *anacrolixTorrent) Stats() TorrentStat {
	s := a.t.Stats()
	return TorrentStat{
		ConnectedSeeders: s.ConnectedSeeders,
		ActivePeers:      s.ActivePeers,
		TotalPeers:       s.TotalPeers,
	}
}

func (a *anacrolixTorrent) Files() []TorrentFile {
	files := a.t.Files()
	out := make([]TorrentFile, 0, len(files))
	for _, f := range files {
		out = append(out, &anacrolixFile{f: f})
	}
	return out
}

type anacrolixFile struct{ f *torrent.File }

func (a *anacrolixFile) Path() string          { return a.f.Path() }
func (a *anacrolixFile) Length() int64         { return a.f.Length() }
func (a *anacrolixFile) BytesCompleted() int64 { return a.f.BytesCompleted() }
func (a *anacrolixFile) NewReader() Reader {
	r := a.f.NewReader()
	// Responsive readers prioritise the region currently being read, which is
	// exactly what streaming/seeking wants.
	r.SetResponsive()
	return r
}
