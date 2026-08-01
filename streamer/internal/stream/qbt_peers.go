package stream

import (
	"context"
	"log"
	"net"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// qbtPeerPollInterval is how often we query qBittorrent for new peers while
// waiting for anacrolix to receive torrent metadata.
const qbtPeerPollInterval = 3 * time.Second

// qbtMaxDuration caps how long we keep a torrent in qBittorrent. If anacrolix
// has not received metadata within this window, we give up and remove the
// probe torrent. The GC will eventually evict the anacrolix session.
const qbtMaxDuration = 2 * time.Minute

// QBitPeerSource uses a running qBittorrent instance as a peer discovery
// accelerator. For each new stream session it adds the magnet to qBittorrent,
// polls the peer list every few seconds, and injects the addresses directly
// into anacrolix — bypassing the DHT cold-start that makes anacrolix slow.
//
// qBittorrent is removed from the probe torrent (with its downloaded data) as
// soon as anacrolix receives metadata or the max duration elapses.
// qbtPeerAPI is the narrow slice of *qbt.Client this source actually uses,
// defined locally (mirroring qbtAPI in qbt_client.go) so tests can substitute
// a fake — no live qBittorrent instance needed. The real *qbt.Client
// satisfies this interface structurally.
type qbtPeerAPI interface {
	AddTorrentFromUrlCtx(ctx context.Context, url string, options map[string]string) (*qbt.TorrentAddResponse, error)
	DeleteTorrentsCtx(ctx context.Context, hashes []string, deleteFiles bool) error
	GetTorrentsCtx(ctx context.Context, o qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	GetTorrentPeersCtx(ctx context.Context, hash string, rid int64) (*qbt.TorrentPeersResponse, error)
}

type QBitPeerSource struct {
	client qbtPeerAPI
	// protectedCategories are the category tags of any qBittorrent-backed
	// features that might be live against the same qBittorrent instance —
	// today the streaming engine's QBitCategory (qbt_client.go) and the
	// download-manager's DownloadQBitCategory (download_manager.go), whichever
	// are actually configured. This is only ever non-empty as a defensive
	// guard: this source is wired up only when STREAM_ENGINE=anacrolix, so it
	// should never encounter a torrent tagged with either — but if two
	// overlapping processes (e.g. an outgoing anacrolix container and an
	// incoming qbittorrent-engine container during a redeploy) end up pointed
	// at the same qBittorrent instance, this stops the peer-accelerator's
	// cleanup from deleting that other feature's live data
	// (docs/STREAMING.md §5 Decision #12, generalized to a set by §6
	// Decision #24 once the download manager introduced a second category).
	protectedCategories []string
}

// NewQBitPeerSource creates a QBitPeerSource, connects to the running
// qBittorrent instance, and logs in. Returns an error if login fails.
// protectedCategories are the category tags of any other qBittorrent-backed
// features that might be live against this qBittorrent instance (may be
// empty/contain empty strings if those features are never used against it) —
// see the cross-process safety note on QBitPeerSource above.
func NewQBitPeerSource(host, user, pass string, protectedCategories []string) (*QBitPeerSource, error) {
	c := qbt.NewClient(qbt.Config{
		Host:     host,
		Username: user,
		Password: pass,
		Timeout:  10,
	})
	if err := c.Login(); err != nil {
		return nil, err
	}
	log.Printf("streamer: qbit peer source connected to %s", host)
	return &QBitPeerSource{client: c, protectedCategories: protectedCategories}, nil
}

// InjectPeers adds the magnet to qBittorrent, polls for peers on a ticker,
// and injects each batch into anacrolix via adder. It runs until gotInfo
// closes (metadata arrived) or qbtMaxDuration elapses, then removes the
// probe torrent from qBittorrent (including any partially downloaded data).
//
// infohash must be the lowercase hex SHA1 infohash (what qBittorrent uses as
// the torrent hash in its API).
func (q *QBitPeerSource) InjectPeers(magnet, infohash string, gotInfo <-chan struct{}, adder func([]net.UDPAddr)) {
	ctx, cancel := context.WithTimeout(context.Background(), qbtMaxDuration)
	defer cancel()

	opts := map[string]string{
		// Limit download to 20 KB/s — we only need peers, not file data.
		// upLimit is intentionally omitted: with near-zero download there is
		// nothing to upload, so the limit has no practical effect.
		"dlLimit": "20480",
	}
	if _, err := q.client.AddTorrentFromUrlCtx(ctx, magnet, opts); err != nil {
		log.Printf("streamer: qbit add torrent %.8s: %v", infohash, err)
		return
	}
	defer func() {
		dCtx, dCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dCancel()
		if cat, owned := q.ownedByProtectedCategory(dCtx, infohash); owned {
			log.Printf("streamer: qbit probe %.8s belongs to a protected category (%q) — skipping delete", infohash, cat)
			return
		}
		// Delete torrent and any downloaded data when we are done probing.
		if err := q.client.DeleteTorrentsCtx(dCtx, []string{infohash}, true); err != nil {
			log.Printf("streamer: qbit delete probe %.8s: %v", infohash, err)
		}
	}()

	ticker := time.NewTicker(qbtPeerPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gotInfo:
			// Metadata arrived — do one final poll to catch any last peers,
			// then exit so the deferred delete runs.
			q.pollAndInject(ctx, infohash, adder)
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.pollAndInject(ctx, infohash, adder)
		}
	}
}

// ownedByProtectedCategory reports whether infohash currently belongs to any
// of protectedCategories (and, if so, which one) — see the cross-process
// safety note on QBitPeerSource. A lookup failure is treated conservatively
// as "not owned" (proceeds with the normal probe-delete path) since
// protectedCategories is expected to be empty for the overwhelming majority
// of deployments (this source only runs when STREAM_ENGINE=anacrolix) and
// failing safe here would risk leaking probe torrents on every transient API
// error.
func (q *QBitPeerSource) ownedByProtectedCategory(ctx context.Context, infohash string) (string, bool) {
	if len(q.protectedCategories) == 0 {
		return "", false
	}
	torrents, err := q.client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{Hashes: []string{infohash}})
	if err != nil || len(torrents) == 0 {
		return "", false
	}
	for _, cat := range q.protectedCategories {
		if cat != "" && torrents[0].Category == cat {
			return cat, true
		}
	}
	return "", false
}

func (q *QBitPeerSource) pollAndInject(ctx context.Context, infohash string, adder func([]net.UDPAddr)) {
	// rid=0 always requests a full peer list (no incremental diffing needed
	// here — anacrolix deduplicates peers by address internally).
	resp, err := q.client.GetTorrentPeersCtx(ctx, infohash, 0)
	if err != nil {
		return
	}
	if resp == nil || len(resp.Peers) == 0 {
		return
	}

	peers := make([]net.UDPAddr, 0, len(resp.Peers))
	for _, p := range resp.Peers {
		if p.IP == "" || p.Port == 0 {
			continue
		}
		ip := net.ParseIP(p.IP)
		if ip == nil {
			continue
		}
		peers = append(peers, net.UDPAddr{IP: ip, Port: p.Port})
	}
	if len(peers) == 0 {
		return
	}
	log.Printf("streamer: qbit injecting %d peers for %.8s", len(peers), infohash)
	adder(peers)
}
