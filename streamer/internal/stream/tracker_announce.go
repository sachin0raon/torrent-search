package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"strings"

	"github.com/anacrolix/torrent/tracker"
	"github.com/anacrolix/torrent/types"
	"github.com/anacrolix/torrent/types/infohash"
)

// announceTrackerLimit caps how many trackers we query per proactive announce.
// More trackers mean more peers but also more goroutines and network load.
const announceTrackerLimit = 50

// announcePeers fires parallel HTTP announces to the given tracker URLs and
// returns the union of all peer addresses found. It is fire-and-collect: each
// tracker gets its own goroutine with the shared context deadline, and errors
// are silently discarded (at least one responding tracker is enough).
//
// Only HTTP/HTTPS tracker URLs are used; UDP trackers are skipped (they need
// a stateful two-step connect+announce that is harder to do out-of-band).
func announcePeers(ctx context.Context, infohashHex string, port uint16, trackerURLs []string) []net.UDPAddr {
	ihBytes, err := hex.DecodeString(infohashHex)
	if err != nil || len(ihBytes) != 20 {
		return nil
	}
	var ih infohash.T
	copy(ih[:], ihBytes)

	var peerID types.PeerID
	_, _ = rand.Read(peerID[:])

	targets := httpTrackers(trackerURLs, announceTrackerLimit)
	if len(targets) == 0 {
		return nil
	}

	type result struct{ addrs []net.UDPAddr }
	ch := make(chan result, len(targets))
	for _, u := range targets {
		go func(url string) {
			resp, err := tracker.Announce{
				TrackerUrl: url,
				Request: tracker.AnnounceRequest{
					InfoHash: ih,
					PeerId:   peerID,
					Left:     1,
					NumWant:  100,
					Port:     port,
					Event:    tracker.Started,
				},
				Context: ctx,
			}.Do()
			if err != nil {
				ch <- result{}
				return
			}
			addrs := make([]net.UDPAddr, 0, len(resp.Peers))
			for _, p := range resp.Peers {
				addrs = append(addrs, net.UDPAddr{IP: p.IP, Port: p.Port})
			}
			ch <- result{addrs: addrs}
		}(u)
	}

	var all []net.UDPAddr
	for range targets {
		r := <-ch
		all = append(all, r.addrs...)
	}
	if len(all) > 0 {
		log.Printf("streamer: proactive announce: %d peers from %d trackers for %.8s",
			len(all), len(targets), infohashHex)
	}
	return all
}

// httpTrackers filters urls to HTTP/HTTPS entries and caps the result at limit.
func httpTrackers(urls []string, limit int) []string {
	out := make([]string, 0, limit)
	for _, u := range urls {
		if len(out) >= limit {
			break
		}
		lo := strings.ToLower(u)
		if strings.HasPrefix(lo, "http://") || strings.HasPrefix(lo, "https://") {
			out = append(out, u)
		}
	}
	return out
}
