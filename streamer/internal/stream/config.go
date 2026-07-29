package stream

import (
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
	// DHTStateFile is the path where the DHT routing table is persisted between
	// restarts. Empty string disables persistence. Defaults to the persistent
	// data volume so the routing table survives container restarts.
	DHTStateFile string
}

// LoadConfig reads configuration from the environment, applying defaults for
// anything unset.
func LoadConfig() Config {
	return Config{
		MaxActive:       envInt("STREAM_MAX_ACTIVE", 5),
		IdleTimeout:     envSeconds("STREAM_IDLE_TIMEOUT", 600),
		MetadataTimeout: envSeconds("STREAM_METADATA_TIMEOUT", 45),
		DownloadDir:     envStr("STREAM_DOWNLOAD_DIR", "/downloads"),
		ListenAddr:      envStr("STREAM_LISTEN_ADDR", "127.0.0.1:8001"),
		GCInterval:      envSeconds("STREAM_GC_INTERVAL", 30),
		TrackersURLs:    trackerURLs(),
		TrackersRefresh: envSeconds("STREAM_TRACKERS_REFRESH", 21600), // 6h
		TrackersTimeout: envSeconds("STREAM_TRACKERS_TIMEOUT", 15),
		TorrentPort:  envInt("STREAM_TORRENT_PORT", 6881),
		DHTStateFile: envStr("STREAM_DHT_STATE_FILE", "/data/dht-state.json"),
	}
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
