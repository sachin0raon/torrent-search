package stream

import (
	"testing"
	"time"
)

func TestTrackerURLs(t *testing.T) {
	t.Run("empty falls back to defaults", func(t *testing.T) {
		t.Setenv("STREAM_TRACKERS_URLS", "")
		if got := trackerURLs(); len(got) != len(DefaultTrackerURLs) {
			t.Errorf("empty should yield %d defaults, got %d", len(DefaultTrackerURLs), len(got))
		}
	})

	t.Run("none disables", func(t *testing.T) {
		for _, v := range []string{"none", "NONE", "off", "disabled"} {
			t.Setenv("STREAM_TRACKERS_URLS", v)
			if got := trackerURLs(); got != nil {
				t.Errorf("%q should disable trackers, got %v", v, got)
			}
		}
	})

	t.Run("explicit list is parsed and trimmed", func(t *testing.T) {
		t.Setenv("STREAM_TRACKERS_URLS", " udp://a:80 , http://b/announce ")
		got := trackerURLs()
		if len(got) != 2 || got[0] != "udp://a:80" || got[1] != "http://b/announce" {
			t.Errorf("unexpected list: %v", got)
		}
	})
}

func TestLoadConfig_QBitEngineDefaults(t *testing.T) {
	for _, v := range []string{
		"STREAM_ENGINE", "STREAM_QBIT_REMOTE_ROOT", "STREAM_QBIT_DOWNLOAD_DIR",
		"STREAM_QBIT_CATEGORY", "STREAM_QBIT_POLL_INTERVAL",
	} {
		t.Setenv(v, "")
	}
	cfg := LoadConfig()
	if cfg.Engine != "anacrolix" {
		t.Errorf("Engine default = %q, want %q", cfg.Engine, "anacrolix")
	}
	if cfg.QBitRemoteRoot != "" {
		t.Errorf("QBitRemoteRoot default = %q, want empty", cfg.QBitRemoteRoot)
	}
	if cfg.QBitDownloadDir != "" {
		t.Errorf("QBitDownloadDir default = %q, want empty", cfg.QBitDownloadDir)
	}
	if cfg.QBitCategory != "tsa-stream-engine" {
		t.Errorf("QBitCategory default = %q, want %q", cfg.QBitCategory, "tsa-stream-engine")
	}
	if cfg.QBitPollInterval != time.Second {
		t.Errorf("QBitPollInterval default = %v, want 1s", cfg.QBitPollInterval)
	}
}

func TestLoadConfig_QBitEngineExplicit(t *testing.T) {
	t.Setenv("STREAM_ENGINE", "qbittorrent")
	t.Setenv("STREAM_QBIT_REMOTE_ROOT", "/data/downloads")
	t.Setenv("STREAM_QBIT_DOWNLOAD_DIR", "/mnt/qbit-downloads")
	t.Setenv("STREAM_QBIT_CATEGORY", "my-category")
	t.Setenv("STREAM_QBIT_POLL_INTERVAL", "2")

	cfg := LoadConfig()
	if cfg.Engine != "qbittorrent" {
		t.Errorf("Engine = %q, want %q", cfg.Engine, "qbittorrent")
	}
	if cfg.QBitRemoteRoot != "/data/downloads" {
		t.Errorf("QBitRemoteRoot = %q", cfg.QBitRemoteRoot)
	}
	if cfg.QBitDownloadDir != "/mnt/qbit-downloads" {
		t.Errorf("QBitDownloadDir = %q", cfg.QBitDownloadDir)
	}
	if cfg.QBitCategory != "my-category" {
		t.Errorf("QBitCategory = %q", cfg.QBitCategory)
	}
	if cfg.QBitPollInterval != 2*time.Second {
		t.Errorf("QBitPollInterval = %v, want 2s", cfg.QBitPollInterval)
	}
}
