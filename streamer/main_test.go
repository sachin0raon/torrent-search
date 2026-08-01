package main

import (
	"testing"
	"time"

	"github.com/sachin0raon/tsa/streamer/internal/stream"
)

// TestBuildClient_QBittorrentEngineNeverWiresPeerSource is the safety-invariant
// test required by docs/STREAMING.md §5 Decision #12: QBitPeerSource must never
// be wired up when the qbittorrent engine is the one actually running, since
// both would target the same qBittorrent instance and the peer-accelerator's
// cleanup deletes torrents by hash — it can't distinguish its own probe torrent
// from the real engine's live session. This must hold even when QBitHost is
// also set (it always is, in this configuration — the qbittorrent engine reuses
// the same connection details).
func TestBuildClient_QBittorrentEngineNeverWiresPeerSource(t *testing.T) {
	cfg := stream.Config{
		Engine: "qbittorrent",
		// Deliberately unreachable: buildClient is expected to fail here (no live
		// qBittorrent instance in this test), but regardless of that failure, it
		// must never return a non-nil QBitPeerSource on this branch.
		QBitHost:         "http://127.0.0.1:1",
		QBitDownloadDir:  t.TempDir(),
		QBitCategory:     "test-engine",
		QBitPollInterval: time.Second,
		IdleTimeout:      time.Minute,
	}

	client, qbit, err := buildClient(cfg)
	if err == nil {
		t.Fatal("expected an error connecting to an unreachable qBittorrent host")
	}
	if client != nil {
		t.Error("expected a nil client alongside the connection error")
	}
	if qbit != nil {
		t.Fatal("qbittorrent engine branch must never return a non-nil QBitPeerSource")
	}
}
