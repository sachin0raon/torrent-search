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

func main() {
	cfg := stream.LoadConfig()

	client, err := stream.NewAnacrolixClient(cfg.DownloadDir, cfg.TorrentPort, cfg.DHTStateFile)
	if err != nil {
		log.Fatalf("streamer: failed to start torrent client: %v", err)
	}

	mgr := stream.NewManager(cfg, client)

	if cfg.QBitHost != "" {
		qbit, err := stream.NewQBitPeerSource(cfg.QBitHost, cfg.QBitUser, cfg.QBitPass)
		if err != nil {
			log.Printf("streamer: qbit peer source unavailable: %v (peer injection disabled)", err)
		} else {
			mgr.SetQBitPeerSource(qbit)
		}
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

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           stream.NewHandler(mgr, cfg).Routes(),
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
}
