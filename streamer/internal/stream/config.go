package stream

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the streamer service's runtime knobs, all sourced from the
// environment with sensible defaults (see docs/STREAMING.md §4.6).
type Config struct {
	// MaxActive caps the number of simultaneously active torrents.
	MaxActive int
	// IdleTimeout is how long a session may go without a stream read before
	// it is dropped and its data deleted.
	IdleTimeout time.Duration
	// MetadataTimeout bounds how long we wait for a magnet's metadata.
	MetadataTimeout time.Duration
	// DownloadDir is the ephemeral root for in-flight torrent data.
	DownloadDir string
	// ListenAddr is the address the HTTP server binds to. It stays on
	// localhost because nginx is the only public listener.
	ListenAddr string
	// GCInterval is how often the idle-GC loop scans for stale sessions.
	GCInterval time.Duration
	// TrackersURLs are public tracker-list URLs fetched to boost peer discovery.
	// Empty disables extra trackers (magnet trackers + DHT still apply).
	TrackersURLs []string
	// TrackersRefresh is how often the tracker lists are re-fetched.
	TrackersRefresh time.Duration
	// TrackersTimeout bounds each tracker-list HTTP fetch.
	TrackersTimeout time.Duration
	// TorrentPort is the fixed TCP+UDP port the anacrolix torrent client listens
	// on for peer connections. Must be reachable from the internet (firewall +
	// Docker port mapping) so peers can initiate connections and push metadata.
	TorrentPort int
	// HalfOpenConnsPerTorrent caps simultaneous outbound connections per torrent.
	HalfOpenConnsPerTorrent int
	// TotalHalfOpenConns caps global simultaneous outbound connections.
	TotalHalfOpenConns int
	// EstablishedConnsPerTorrent caps total established connections per torrent.
	EstablishedConnsPerTorrent int
	// DHTStateFile is the path where the DHT routing table is persisted between
	// restarts. Empty string disables persistence. Defaults to the persistent
	// data volume so the routing table survives container restarts.
	DHTStateFile string
	// QBitHost is the base URL of a running qBittorrent Web UI
	// (e.g. "http://localhost:8080"). Empty string disables qBittorrent peer
	// injection entirely (anacrolix engine) or is required (qbittorrent engine).
	QBitHost string
	// QBitUser and QBitPass are the Web UI credentials (default: admin / adminadmin).
	QBitUser string
	QBitPass string

	// Engine selects the BitTorrent download engine: "anacrolix" (default) or
	// "qbittorrent". Anything else falls back to "anacrolix".
	Engine string
	// QBitRemoteRoot is qBittorrent's save-path root as qBittorrent itself sees it
	// (e.g. "/data/downloads"). Required when Engine is "qbittorrent".
	QBitRemoteRoot string
	// QBitDownloadDir is the local filesystem root the streamer container sees for
	// that same directory (the bind-mount target). Required when Engine is
	// "qbittorrent".
	QBitDownloadDir string
	// QBitCategory tags torrents the qbittorrent engine adds, so a restart can
	// purge orphans from a prior crash without touching unrelated torrents in a
	// shared qBittorrent instance.
	QBitCategory string
	// QBitPollInterval is how often the qbittorrent engine polls for
	// metadata-readiness and piece-state.
	QBitPollInterval time.Duration
	// QBitPauseTimeout is how long a qbittorrent-engine session may go idle
	// before it is paused (download+upload stopped) rather than removed —
	// docs/STREAMING.md §7.
	QBitPauseTimeout time.Duration
	// QBitRetentionTimeout is how long a paused qbittorrent-engine session may
	// stay paused before it is actually removed, measured from when it was
	// paused. Applies uniformly to complete and incomplete downloads
	// (docs/STREAMING.md §7 Decision #30).
	QBitRetentionTimeout time.Duration

	// DownloadEngine enables the persistent download-manager feature when set
	// to "qbittorrent". Empty (default) means the feature is entirely absent.
	// Independent of Engine — see docs/STREAMING.md §6.
	DownloadEngine string
	// DownloadQBitCategory tags torrents the download-manager feature adds.
	// Unlike QBitCategory, this is never purged on startup: downloads are
	// intentionally persistent (Decision #22).
	DownloadQBitCategory string
	// DownloadUnselectedTimeout is how long a download-manager torrent may
	// sit with no file selected (e.g. the user opened the file picker to see
	// what's in a magnet, then never picked anything) before it's
	// automatically removed. Does not apply once at least one file has been
	// selected — that torrent is a real, intentional download and is never
	// swept (Decision #26).
	DownloadUnselectedTimeout time.Duration
}

// LoadConfig reads configuration from the environment, applying defaults for
// anything unset.
func LoadConfig() Config {
	return Config{
		MaxActive:                  envInt("STREAM_MAX_ACTIVE", 5),
		IdleTimeout:                envSeconds("STREAM_IDLE_TIMEOUT", 600),
		MetadataTimeout:            envSeconds("STREAM_METADATA_TIMEOUT", 45),
		DownloadDir:                envStr("STREAM_DOWNLOAD_DIR", "/downloads"),
		ListenAddr:                 envStr("STREAM_LISTEN_ADDR", "127.0.0.1:8001"),
		GCInterval:                 envSeconds("STREAM_GC_INTERVAL", 30),
		TrackersURLs:               trackerURLs(),
		TrackersRefresh:            envSeconds("STREAM_TRACKERS_REFRESH", 21600), // 6h
		TrackersTimeout:            envSeconds("STREAM_TRACKERS_TIMEOUT", 15),
		TorrentPort:                envInt("STREAM_TORRENT_PORT", 6881),
		HalfOpenConnsPerTorrent:    envInt("STREAM_HALF_OPEN_CONNS_PER_TORRENT", 100),
		TotalHalfOpenConns:         envInt("STREAM_TOTAL_HALF_OPEN_CONNS", 500),
		EstablishedConnsPerTorrent: envInt("STREAM_ESTABLISHED_CONNS_PER_TORRENT", 200),
		DHTStateFile:               envStr("STREAM_DHT_STATE_FILE", "/data/dht-state.json"),
		QBitHost:                   envStr("STREAM_QBIT_HOST", ""),
		QBitUser:                   envStr("STREAM_QBIT_USER", "admin"),
		QBitPass:                   envStr("STREAM_QBIT_PASS", "adminadmin"),

		Engine:               envStr("STREAM_ENGINE", "anacrolix"),
		QBitRemoteRoot:       envStr("STREAM_QBIT_REMOTE_ROOT", ""),
		QBitDownloadDir:      envStr("STREAM_QBIT_DOWNLOAD_DIR", ""),
		QBitCategory:         envStr("STREAM_QBIT_CATEGORY", "tsa-stream-engine"),
		QBitPollInterval:     envSeconds("STREAM_QBIT_POLL_INTERVAL", 1),
		QBitPauseTimeout:     envSeconds("STREAM_QBIT_PAUSE_TIMEOUT", 60),
		QBitRetentionTimeout: envSeconds("STREAM_QBIT_RETENTION_TIMEOUT", 86400),

		DownloadEngine:            envStr("DOWNLOAD_ENGINE", ""),
		DownloadQBitCategory:      envStr("DOWNLOAD_QBIT_CATEGORY", "tsa-download"),
		DownloadUnselectedTimeout: envSeconds("DOWNLOAD_UNSELECTED_TIMEOUT", 900), // 15m
	}
}

// ValidateEngines rejects invalid combinations of Engine and DownloadEngine.
// STREAM_ENGINE=qbittorrent and DOWNLOAD_ENGINE=qbittorrent can never both be
// active in the same process (docs/STREAMING.md §6 Decision #25): nothing in
// the design needs two qBittorrent-backed engines against the same instance,
// and allowing it would add a permanently-live edge case for no functional
// benefit. Pure/no I/O so it can run before any network call.
func (c Config) ValidateEngines() error {
	if c.Engine == "qbittorrent" && c.DownloadEngine == "qbittorrent" {
		return fmt.Errorf("STREAM_ENGINE=qbittorrent and DOWNLOAD_ENGINE=qbittorrent cannot both be enabled — use STREAM_ENGINE=anacrolix with DOWNLOAD_ENGINE=qbittorrent instead")
	}
	return nil
}

// trackerURLs resolves STREAM_TRACKERS_URLS. Unset or empty → the built-in
// defaults; a comma-separated list → that list; the literal "none"/"off" →
// disabled (no extra trackers). Empty maps to defaults (not "disabled") because
// docker-compose always injects the var as an empty string.
func trackerURLs() []string {
	urls := envList("STREAM_TRACKERS_URLS", DefaultTrackerURLs)
	if len(urls) == 1 {
		switch strings.ToLower(urls[0]) {
		case "none", "off", "disabled":
			return nil
		}
	}
	return urls
}

// envList parses a comma-separated env var into a trimmed, non-empty list,
// falling back to def when unset.
func envList(key string, def []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envSeconds(key string, def int) time.Duration {
	return time.Duration(envInt(key, def)) * time.Second
}
