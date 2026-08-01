package stream

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// Sentinel errors returned by DownloadManager, mapped to HTTP status codes by
// download_handlers.go.
var (
	ErrDownloadInvalidMagnet   = errors.New("download: invalid magnet")
	ErrDownloadMetadataTimeout = errors.New("download: timed out fetching torrent metadata")
	ErrDownloadNotFound        = errors.New("download: torrent not found")
)

// DownloadInfo describes one torrent tracked by the download manager, for the
// list/detail API responses.
type DownloadInfo struct {
	Hash     string             `json:"hash"`
	Name     string             `json:"name"`
	State    string             `json:"state"`
	Progress float64            `json:"progress"`
	DlSpeed  int64              `json:"dlspeed"`
	Size     int64              `json:"size"`
	Files    []DownloadFileInfo `json:"files,omitempty"`
}

// DownloadFileInfo describes one file within a download-manager torrent.
type DownloadFileInfo struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Downloaded int64  `json:"downloaded"`
	// Selected reflects qBittorrent's own per-file priority (priority > 0),
	// not a separate record we keep ourselves — see the package doc on
	// DownloadManager for why.
	Selected bool `json:"selected"`
}

// DownloadManager talks to qBittorrent on behalf of the persistent
// download-manager feature (docs/STREAMING.md §6). Unlike Manager (the
// streaming path), it holds no session state of its own — qBittorrent is the
// sole source of truth, queried live on every call, so the download list
// survives a streamer restart for free (§6.2 Assumption #2).
type DownloadManager struct {
	api          qbtAPI
	remoteRoot   string
	downloadDir  string
	category     string
	pollInterval time.Duration

	// unselectedTimeout and now back PurgeUnselected's idle sweep (Decision
	// #26) — see StartGC.
	unselectedTimeout time.Duration
	now               func() time.Time // injectable clock for tests

	stopGC chan struct{}
	// gcCancel cancels the context passed to an in-flight PurgeUnselected
	// sweep. Set by StartGC (nil if it was never called); Close invokes it so
	// a slow sweep (many torrents, each API call individually bounded by
	// apiTimeout) can't make shutdown block for the sweep's full remaining
	// duration — each call fails fast once its context is cancelled instead.
	gcCancel context.CancelFunc
	wg       sync.WaitGroup
}

// NewDownloadManager builds a DownloadManager backed by a running qBittorrent
// instance. It logs in and validates downloadDir is a readable local
// directory (same fail-fast posture as NewQBitClient), but deliberately does
// NOT purge category on startup (Decision #22) — downloads must survive a
// streamer restart, the opposite lifecycle of the streaming engine's own
// category.
func NewDownloadManager(host, user, pass, remoteRoot, downloadDir, category string, pollInterval, unselectedTimeout time.Duration) (*DownloadManager, error) {
	api := qbt.NewClient(qbt.Config{Host: host, Username: user, Password: pass, Timeout: 10})
	return newDownloadManagerWithAPI(api, remoteRoot, downloadDir, category, pollInterval, unselectedTimeout)
}

// newDownloadManagerWithAPI holds the fail-fast startup logic against the
// qbtAPI interface rather than the concrete *qbt.Client, so it's unit-testable
// with a fake — no live qBittorrent instance needed.
func newDownloadManagerWithAPI(api qbtAPI, remoteRoot, downloadDir, category string, pollInterval, unselectedTimeout time.Duration) (*DownloadManager, error) {
	loginCtx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	if err := api.LoginCtx(loginCtx); err != nil {
		return nil, fmt.Errorf("download: login: %w", err)
	}

	info, err := os.Stat(downloadDir)
	if err != nil {
		return nil, fmt.Errorf("download: STREAM_QBIT_DOWNLOAD_DIR %q: %w", downloadDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("download: STREAM_QBIT_DOWNLOAD_DIR %q is not a directory", downloadDir)
	}

	return &DownloadManager{
		api:               api,
		remoteRoot:        remoteRoot,
		downloadDir:       downloadDir,
		category:          category,
		pollInterval:      pollInterval,
		unselectedTimeout: unselectedTimeout,
		now:               time.Now,
		stopGC:            make(chan struct{}),
	}, nil
}

// SetClock overrides the clock used for the unselected-timeout check (tests only).
func (m *DownloadManager) SetClock(now func() time.Time) { m.now = now }

// StartGC launches the background sweep that removes torrents no file has
// ever been selected for (PurgeUnselected), every interval. The context
// handed to each sweep is cancelled by Close, not left as
// context.Background() — PurgeUnselected can make several sequential,
// apiTimeout-bounded API calls per torrent, and without this, Close could
// block for the sweep's entire remaining duration instead of returning
// promptly on shutdown.
func (m *DownloadManager) StartGC(interval time.Duration) {
	gcCtx, cancel := context.WithCancel(context.Background())
	m.gcCancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopGC:
				return
			case <-ticker.C:
				if _, err := m.PurgeUnselected(gcCtx); err != nil && gcCtx.Err() == nil {
					log.Printf("streamer: download purge-unselected sweep: %v", err)
				}
			}
		}
	}()
}

// Close stops the GC loop, cancelling any in-flight sweep so it unblocks
// promptly instead of running to completion, then waits for the goroutine to
// exit. Safe to call even if StartGC was never invoked.
func (m *DownloadManager) Close() {
	close(m.stopGC)
	if m.gcCancel != nil {
		m.gcCancel()
	}
	m.wg.Wait()
}

// PurgeUnselected removes any category-tagged torrent that was added at
// least unselectedTimeout ago and still has no file selected — e.g. a user
// opened the file picker to see what's in a magnet and then never picked
// anything (§6 Decision #26). A torrent whose metadata never arrived (so it
// has no files at all yet) is vacuously "nothing selected" and is swept the
// same way, which also cleans up dead/unreachable magnets. A torrent with
// at least one selected file is a real, intentional download and is never
// touched here, regardless of age. Returns the hashes it removed (for
// logging and tests).
func (m *DownloadManager) PurgeUnselected(ctx context.Context) ([]string, error) {
	listCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	torrents, err := m.api.GetTorrentsCtx(listCtx, qbt.TorrentFilterOptions{Category: m.category})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("download: purge-unselected list: %w", err)
	}

	cutoff := m.now().Add(-m.unselectedTimeout).Unix()
	var removed []string
	for _, t := range torrents {
		if ctx.Err() != nil {
			// Cancelled mid-sweep (Close, during shutdown) — stop touching
			// more torrents rather than let every remaining one fail its API
			// calls one at a time before the loop naturally ends.
			break
		}
		if t.AddedOn > cutoff {
			continue // not old enough yet
		}

		filesCtx, cancel := context.WithTimeout(ctx, apiTimeout)
		files, err := m.api.GetFilesInformationCtx(filesCtx, t.Hash)
		cancel()
		if err != nil {
			// Transient failure — leave it for the next tick rather than risk
			// deleting something we couldn't actually verify was unselected.
			log.Printf("streamer: download purge-unselected check hash=%.8s: %v (skipping this tick)", t.Hash, err)
			continue
		}
		if hasSelectedFile(files) {
			continue
		}

		delCtx, cancel2 := context.WithTimeout(ctx, apiTimeout)
		err = m.api.DeleteTorrentsCtx(delCtx, []string{t.Hash}, true)
		cancel2()
		if err != nil {
			log.Printf("streamer: download purge-unselected delete hash=%.8s: %v", t.Hash, err)
			continue
		}
		log.Printf("streamer: download purge-unselected removed hash=%.8s name=%q (never selected)", t.Hash, t.Name)
		removed = append(removed, t.Hash)
	}
	return removed, nil
}

// hasSelectedFile reports whether any file has been promoted above priority
// 0. nil/empty (metadata not yet arrived) counts as "nothing selected."
func hasSelectedFile(files *qbt.TorrentFiles) bool {
	if files == nil {
		return false
	}
	for _, f := range *files {
		if f.Priority > 0 {
			return true
		}
	}
	return false
}

// AddTorrent adds a magnet tagged with the download category and blocks
// (bounded by ctx) until its metadata is available, then zeroes every file's
// download priority — nothing downloads until an explicit SelectFiles call,
// mirroring qbtTorrent.pollMetadata's "only the picked file downloads"
// baseline for the streaming engine (Decision #6). Unlike Manager.AddSession,
// this runs synchronously in the calling goroutine rather than via a
// background poller + channel: it's called directly from a single blocking
// HTTP handler, so there's no async session model to feed.
func (m *DownloadManager) AddTorrent(ctx context.Context, magnet string) (DownloadInfo, error) {
	clean := sanitizeMagnet(magnet)
	hash, err := parseMagnetInfohash(clean)
	if err != nil {
		return DownloadInfo{}, ErrDownloadInvalidMagnet
	}

	// A season pack's magnet may already be tracked from an earlier AddTorrent
	// call for a different file in the same pack — qBittorrent rejects
	// re-adding a hash it already knows with an HTTP 409 ("conflicts
	// detected"). Detect that up front and skip the add, so picking a second
	// file from an already-downloading pack doesn't fail.
	checkCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	existing, err := m.api.GetTorrentsCtx(checkCtx, qbt.TorrentFilterOptions{Hashes: []string{hash}})
	cancel()
	alreadyTracked := err == nil && len(existing) > 0

	if !alreadyTracked {
		opts := map[string]string{
			"category":           m.category,
			"sequentialDownload": "true",
			"firstLastPiecePrio": "true",
		}
		addCtx, addCancel := context.WithTimeout(ctx, apiTimeout)
		_, err = m.api.AddTorrentFromUrlCtx(addCtx, clean, opts)
		addCancel()
		if err != nil {
			return DownloadInfo{}, fmt.Errorf("download: add torrent: %w", err)
		}
	}

	// Already-tracked torrents use pollExistingOnce, which skips the
	// zero-all-priorities step: the pack may have files already selected and
	// downloading from an earlier SelectFiles call, and re-zeroing them here
	// would silently stop that download.
	poll := m.pollMetadataOnce
	if alreadyTracked {
		poll = m.pollExistingOnce
	}

	// Check once immediately before entering the ticker loop below — a ticker's
	// first tick only fires after a full pollInterval, which would otherwise
	// delay even already-available metadata (e.g. re-adding a hash qBittorrent
	// already knows) by a needless wait.
	if info, ready := poll(ctx, hash); ready {
		return info, nil
	}

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return DownloadInfo{}, ErrDownloadMetadataTimeout
		case <-ticker.C:
			info, ready := poll(ctx, hash)
			if ready {
				return info, nil
			}
		}
	}
}

// fetchReadyMetadata checks whether hash's metadata (piece count + file list)
// is available yet, returning it if so. API errors are treated as transient —
// AddTorrent's outer ctx deadline remains the sole give-up point, consistent
// with the streaming engine's metadata poller (Decision #15).
func (m *DownloadManager) fetchReadyMetadata(ctx context.Context, hash string) (qbt.TorrentProperties, qbt.TorrentFiles, bool) {
	propsCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	props, err := m.api.GetTorrentPropertiesCtx(propsCtx, hash)
	cancel()
	if err != nil || props.PiecesNum <= 0 {
		return qbt.TorrentProperties{}, nil, false
	}

	filesCtx, cancel2 := context.WithTimeout(ctx, apiTimeout)
	files, err := m.api.GetFilesInformationCtx(filesCtx, hash)
	cancel2()
	if err != nil || files == nil || len(*files) == 0 {
		return qbt.TorrentProperties{}, nil, false
	}
	return props, *files, true
}

// pollMetadataOnce is fetchReadyMetadata for a freshly-added torrent: once
// metadata is available it zeroes every file's priority and returns the ready
// DownloadInfo — nothing downloads until an explicit SelectFiles call.
func (m *DownloadManager) pollMetadataOnce(ctx context.Context, hash string) (DownloadInfo, bool) {
	props, files, ready := m.fetchReadyMetadata(ctx, hash)
	if !ready {
		return DownloadInfo{}, false
	}

	ids := make([]string, len(files))
	for i := range files {
		ids[i] = strconv.Itoa(i)
	}
	zeroCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	_ = m.api.SetFilePriorityCtx(zeroCtx, hash, strings.Join(ids, "|"), 0)
	cancel()

	return DownloadInfo{
		Hash:  hash,
		Name:  props.Name,
		Files: buildDownloadFiles(files),
	}, true
}

// pollExistingOnce is fetchReadyMetadata for a torrent AddTorrent found
// already tracked (e.g. a second file picked from the same season-pack
// magnet). Unlike pollMetadataOnce it never touches file priorities — the
// pack may already have files selected and downloading from an earlier
// SelectFiles call, and zeroing them here would silently stop that download.
func (m *DownloadManager) pollExistingOnce(ctx context.Context, hash string) (DownloadInfo, bool) {
	props, files, ready := m.fetchReadyMetadata(ctx, hash)
	if !ready {
		return DownloadInfo{}, false
	}
	return DownloadInfo{
		Hash:  hash,
		Name:  props.Name,
		Files: buildDownloadFiles(files),
	}, true
}

// SelectFiles promotes the given file indices to normal download priority.
// Additive, not exclusive: unlike the streaming engine's PrioritizeFile
// (Decision #6/#14), selecting more files never demotes ones already
// selected — season-pack downloads can pick several files at once with no
// deferred-demotion/refcounting machinery needed (§6.2 Assumption #7).
func (m *DownloadManager) SelectFiles(ctx context.Context, hash string, indices []int) error {
	if hash == "" {
		return ErrDownloadNotFound
	}
	if len(indices) == 0 {
		return nil
	}
	ids := make([]string, len(indices))
	for i, idx := range indices {
		ids[i] = strconv.Itoa(idx)
	}
	selCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	if err := m.api.SetFilePriorityCtx(selCtx, hash, strings.Join(ids, "|"), 1); err != nil {
		return fmt.Errorf("download: select files: %w", err)
	}
	return nil
}

// List returns every torrent tagged with this manager's category — the live
// qBittorrent state backing the Downloads UI's list view. No per-file detail
// (callers use Get for that), keeping the common polled call cheap.
func (m *DownloadManager) List(ctx context.Context) ([]DownloadInfo, error) {
	listCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	torrents, err := m.api.GetTorrentsCtx(listCtx, qbt.TorrentFilterOptions{Category: m.category})
	if err != nil {
		return nil, fmt.Errorf("download: list: %w", err)
	}
	// Most recently added first — qBittorrent's own list order isn't
	// guaranteed to reflect add order, and the Downloads UI wants the
	// torrent you just started at the top rather than buried under older
	// entries.
	sort.Slice(torrents, func(i, j int) bool { return torrents[i].AddedOn > torrents[j].AddedOn })
	out := make([]DownloadInfo, len(torrents))
	for i, t := range torrents {
		out[i] = DownloadInfo{
			Hash:     t.Hash,
			Name:     t.Name,
			State:    string(t.State),
			Progress: t.Progress,
			DlSpeed:  t.DlSpeed,
			Size:     t.Size,
		}
	}
	return out, nil
}

// Get returns one torrent's detail, including per-file progress and
// selection state, for the Downloads UI's expanded/completed view. If the
// torrent-level lookup succeeds but the file-list call fails (a transient
// qBittorrent blip, not a sign the torrent is gone — it was just found),
// Get degrades gracefully and returns the torrent info with Files unset
// rather than failing the whole call: this is the polled detail endpoint,
// so a one-off files-fetch error shouldn't turn a working progress display
// into an error state.
func (m *DownloadManager) Get(ctx context.Context, hash string) (DownloadInfo, error) {
	if hash == "" {
		return DownloadInfo{}, ErrDownloadNotFound
	}
	listCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	torrents, err := m.api.GetTorrentsCtx(listCtx, qbt.TorrentFilterOptions{Hashes: []string{hash}})
	cancel()
	if err != nil {
		return DownloadInfo{}, fmt.Errorf("download: get: %w", err)
	}
	if len(torrents) == 0 {
		return DownloadInfo{}, ErrDownloadNotFound
	}
	t := torrents[0]

	info := DownloadInfo{
		Hash:     t.Hash,
		Name:     t.Name,
		State:    string(t.State),
		Progress: t.Progress,
		DlSpeed:  t.DlSpeed,
		Size:     t.Size,
	}

	filesCtx, cancel2 := context.WithTimeout(ctx, apiTimeout)
	files, err := m.api.GetFilesInformationCtx(filesCtx, hash)
	cancel2()
	if err != nil {
		log.Printf("streamer: download get files hash=%.8s: %v (returning torrent info without file detail)", hash, err)
		return info, nil
	}
	if files != nil {
		info.Files = buildDownloadFiles(*files)
	}
	return info, nil
}

// Pause suspends downloading for the given torrent without removing it.
func (m *DownloadManager) Pause(ctx context.Context, hash string) error {
	if hash == "" {
		return ErrDownloadNotFound
	}
	pauseCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	if err := m.api.PauseCtx(pauseCtx, []string{hash}); err != nil {
		return fmt.Errorf("download: pause: %w", err)
	}
	return nil
}

// Resume restarts a previously paused/stopped torrent.
func (m *DownloadManager) Resume(ctx context.Context, hash string) error {
	if hash == "" {
		return ErrDownloadNotFound
	}
	resumeCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	if err := m.api.ResumeCtx(resumeCtx, []string{hash}); err != nil {
		return fmt.Errorf("download: resume: %w", err)
	}
	return nil
}

// Delete removes the torrent and its downloaded data. Always deletes data —
// there is no "keep files" variant (§6.2 Assumption #8).
func (m *DownloadManager) Delete(ctx context.Context, hash string) error {
	if hash == "" {
		return ErrDownloadNotFound
	}
	delCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	if err := m.api.DeleteTorrentsCtx(delCtx, []string{hash}, true); err != nil {
		return fmt.Errorf("download: delete: %w", err)
	}
	return nil
}

// buildDownloadFiles maps qBittorrent's file list into []DownloadFileInfo,
// ordered by Index. Shared shape with qbtTorrent.buildFiles, but this
// manager has no TorrentFile/Reader abstraction to populate — it only ever
// needs the plain data for JSON responses.
func buildDownloadFiles(raw qbt.TorrentFiles) []DownloadFileInfo {
	sorted := make([]qbt.TorrentFile, len(raw))
	copy(sorted, raw)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Index < sorted[j].Index })

	out := make([]DownloadFileInfo, len(sorted))
	for i, f := range sorted {
		out[i] = DownloadFileInfo{
			Index:      f.Index,
			Name:       f.Name,
			Size:       f.Size,
			Downloaded: int64(float64(f.Progress) * float64(f.Size)),
			Selected:   f.Priority > 0,
		}
	}
	return out
}
