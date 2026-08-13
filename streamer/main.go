// Command streamer is the Go torrent-streaming microservice for the Torrent
// Search Aggregator. It adds magnets via github.com/anacrolix/torrent, lists
// their files, and serves a chosen file over HTTP with range support. It binds
// to localhost and sits behind nginx (see docs/STREAMING.md).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sachin0raon/tsa/streamer/internal/stream"
)

// buildClient selects the download engine per cfg.Engine. Extracted from main()
// so the safety invariant in docs/STREAMING.md §5 Decision #12 — QBitPeerSource
// must never be wired up when the qbittorrent engine is the one actually
// running — is unit-testable: main() itself can't be exercised directly.
//
// The two uses of qBittorrent (peer-accelerator for anacrolix vs. real engine)
// are mutually exclusive: this function returns a non-nil *QBitPeerSource only
// on the anacrolix branch, never on the qbittorrent branch, regardless of
// whether cfg.QBitHost is also set.
func buildClient(cfg stream.Config) (stream.TorrentClient, *stream.QBitPeerSource, error) {
	if cfg.Engine == "qbittorrent" {
		client, err := stream.NewQBitClient(cfg.QBitHost, cfg.QBitUser, cfg.QBitPass,
			cfg.QBitRemoteRoot, cfg.QBitDownloadDir, cfg.QBitCategory, cfg.QBitPollInterval, cfg.IdleTimeout)
		if err != nil {
			return nil, nil, err
		}
		return client, nil, nil
	}

	client, err := stream.NewAnacrolixClient(cfg.DownloadDir, cfg.TorrentPort, cfg.DHTStateFile, cfg.HalfOpenConnsPerTorrent, cfg.TotalHalfOpenConns, cfg.EstablishedConnsPerTorrent)
	if err != nil {
		return nil, nil, err
	}

	var qbit *stream.QBitPeerSource
	if cfg.QBitHost != "" {
		// Protect both the streaming engine's category (only relevant during a
		// redeploy overlap, since this branch only runs when cfg.Engine !=
		// "qbittorrent") and the download manager's, if configured — see
		// docs/STREAMING.md §6 Decision #24.
		protected := []string{cfg.QBitCategory}
		if cfg.DownloadEngine == "qbittorrent" {
			protected = append(protected, cfg.DownloadQBitCategory)
		}
		qbit, err = stream.NewQBitPeerSource(cfg.QBitHost, cfg.QBitUser, cfg.QBitPass, protected)
		if err != nil {
			log.Printf("streamer: qbit peer source unavailable: %v (peer injection disabled)", err)
			qbit = nil
		}
	}
	return client, qbit, nil
}

// buildDownloadManager constructs the persistent download-manager feature
// (docs/STREAMING.md §6) when DOWNLOAD_ENGINE=qbittorrent, independently of
// buildClient's STREAM_ENGINE switch — the two are mutually exclusive at the
// "qbittorrent" value (enforced by Config.ValidateEngines, Decision #25) but
// otherwise unrelated. Returns nil, nil when the feature is off.
func buildDownloadManager(cfg stream.Config) (*stream.DownloadManager, error) {
	if cfg.DownloadEngine != "qbittorrent" {
		return nil, nil
	}
	return stream.NewDownloadManager(cfg.QBitHost, cfg.QBitUser, cfg.QBitPass,
		cfg.QBitRemoteRoot, cfg.QBitDownloadDir, cfg.DownloadQBitCategory, cfg.QBitPollInterval,
		cfg.DownloadUnselectedTimeout)
}

func main() {
	cfg := stream.LoadConfig()

	if err := cfg.ValidateEngines(); err != nil {
		log.Fatalf("streamer: %v", err)
	}

	client, qbit, err := buildClient(cfg)
	if err != nil {
		log.Fatalf("streamer: failed to start torrent client: %v", err)
	}

	downloadMgr, err := buildDownloadManager(cfg)
	if err != nil {
		log.Fatalf("streamer: download engine unavailable: %v", err)
	}

	mgr := stream.NewManager(cfg, client)

	if qbit != nil {
		mgr.SetQBitPeerSource(qbit)
	}

	if err := mgr.WipeDownloadDir(); err != nil {
		log.Fatalf("streamer: failed to prepare download dir: %v", err)
	}
	mgr.StartGC()

	// Fetch public tracker lists (non-fatal) and keep them refreshed, to widen
	// peer discovery for every stream.
	var trackers *stream.TrackerSource
	if len(cfg.TrackersURLs) > 0 {
		trackers = stream.NewTrackerSource(cfg.TrackersURLs, cfg.TrackersTimeout)
		fetchCtx, cancel := context.WithTimeout(context.Background(), cfg.TrackersTimeout)
		if err := trackers.Fetch(fetchCtx); err != nil {
			log.Printf("streamer: initial tracker fetch failed: %v", err)
		} else {
			log.Printf("streamer: loaded %d trackers", trackers.Count())
		}
		cancel()
		trackers.StartRefresh(cfg.TrackersRefresh)
		mgr.SetTrackerProvider(trackers)
	}

	handler := stream.NewHandler(mgr, cfg)
	if downloadMgr != nil {
		handler.SetDownloadManager(downloadMgr)
		downloadMgr.StartGC(cfg.GCInterval)
		log.Printf("streamer: download manager enabled (category=%s, unselected-timeout=%s)",
			cfg.DownloadQBitCategory, cfg.DownloadUnselectedTimeout)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("streamer: listening on %s (max=%d idle=%s)", cfg.ListenAddr, cfg.MaxActive, cfg.IdleTimeout)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("streamer: server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Printf("streamer: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	if trackers != nil {
		trackers.Close()
	}
	mgr.Close()
	if downloadMgr != nil {
		downloadMgr.Close()
	}
}
