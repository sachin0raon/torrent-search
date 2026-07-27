package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultTrackerURLs are the public tracker lists fetched to boost peer
// discovery. Overridable via STREAM_TRACKERS_URLS.
var DefaultTrackerURLs = []string{
	"https://cf.trackerslist.com/all.txt",
	"https://ngosang.github.io/trackerslist/trackers_all_http.txt",
	"https://raw.githubusercontent.com/hezhijie0327/Trackerslist/main/trackerslist_tracker.txt",
}

// TrackerSource fetches and caches public tracker announce URLs, refreshing them
// in the background. It satisfies the Manager's TrackerProvider interface.
type TrackerSource struct {
	urls    []string
	timeout time.Duration
	client  *http.Client

	mu   sync.RWMutex
	list []string

	stop chan struct{}
	wg   sync.WaitGroup
}

// NewTrackerSource builds a source over the given list URLs.
func NewTrackerSource(urls []string, timeout time.Duration) *TrackerSource {
	return &TrackerSource{
		urls:    urls,
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
		stop:    make(chan struct{}),
	}
}

// Fetch downloads all tracker lists concurrently and replaces the cached set.
// It returns an error only if every source failed (the previous set is kept),
// so a partial outage still yields usable trackers.
func (ts *TrackerSource) Fetch(ctx context.Context) error {
	type res struct {
		trackers []string
		err      error
	}
	results := make(chan res, len(ts.urls))
	for _, u := range ts.urls {
		go func(u string) {
			tr, err := ts.fetchOne(ctx, u)
			results <- res{tr, err}
		}(u)
	}

	seen := make(map[string]struct{})
	var merged []string
	failures := 0
	for range ts.urls {
		r := <-results
		if r.err != nil {
			failures++
			continue
		}
		for _, t := range r.trackers {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			merged = append(merged, t)
		}
	}
	if failures == len(ts.urls) {
		return fmt.Errorf("all %d tracker sources failed", len(ts.urls))
	}

	ts.mu.Lock()
	ts.list = merged
	ts.mu.Unlock()
	return nil
}

func (ts *TrackerSource) fetchOne(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := ts.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return parseTrackers(resp.Body)
}

// parseTrackers extracts one tracker URL per non-empty, non-comment line.
func parseTrackers(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if isTrackerURL(line) {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

func isTrackerURL(s string) bool {
	for _, p := range []string{"http://", "https://", "udp://", "ws://", "wss://"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Tiers returns the cached trackers as a one-tracker-per-tier announce list, so
// anacrolix announces to every tracker (BEP-12 sticks to one tracker per tier).
func (ts *TrackerSource) Tiers() [][]string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	tiers := make([][]string, len(ts.list))
	for i, t := range ts.list {
		tiers[i] = []string{t}
	}
	return tiers
}

// Count reports how many trackers are currently cached.
func (ts *TrackerSource) Count() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.list)
}

// StartRefresh periodically re-fetches the lists in the background.
func (ts *TrackerSource) StartRefresh(interval time.Duration) {
	if interval <= 0 {
		return
	}
	ts.wg.Add(1)
	go func() {
		defer ts.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ts.stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), ts.timeout)
				if err := ts.Fetch(ctx); err != nil {
					log.Printf("streamer: tracker refresh failed: %v", err)
				}
				cancel()
			}
		}
	}()
}

// Close stops the background refresh loop.
func (ts *TrackerSource) Close() {
	close(ts.stop)
	ts.wg.Wait()
}
