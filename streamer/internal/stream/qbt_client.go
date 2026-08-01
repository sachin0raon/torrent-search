package stream

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// apiTimeout bounds every individual qBittorrent Web API call made by this
// adapter, so a single unresponsive call can't hang a poller or reader forever.
const apiTimeout = 15 * time.Second

// qbtAPI is the narrow slice of *qbt.Client this adapter actually uses. Defining
// it locally (rather than depending on the concrete *qbt.Client type directly)
// lets tests substitute a fake with no live qBittorrent instance — the real
// *qbt.Client satisfies this interface structurally, no wrapper needed.
type qbtAPI interface {
	LoginCtx(ctx context.Context) error
	AddTorrentFromUrlCtx(ctx context.Context, url string, options map[string]string) (*qbt.TorrentAddResponse, error)
	GetTorrentsCtx(ctx context.Context, o qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	GetTorrentPropertiesCtx(ctx context.Context, hash string) (qbt.TorrentProperties, error)
	GetFilesInformationCtx(ctx context.Context, hash string) (*qbt.TorrentFiles, error)
	GetTorrentPieceStatesCtx(ctx context.Context, hash string) ([]qbt.PieceState, error)
	SetFilePriorityCtx(ctx context.Context, hash string, ids string, priority int) error
	AddTrackersCtx(ctx context.Context, hash string, urls string) error
	DeleteTorrentsCtx(ctx context.Context, hashes []string, deleteFiles bool) error
}

// --- qBittorrent engine adapter (TorrentClient) ---

type qbtClient struct {
	api          qbtAPI
	remoteRoot   string
	downloadDir  string
	category     string
	pollInterval time.Duration
	idleTimeout  time.Duration
}

// NewQBitClient builds a TorrentClient backed by a running qBittorrent instance.
// It logs in, validates that downloadDir is a readable local directory (so a
// misconfigured path-mapping fails fast at startup rather than surfacing only at
// playback — docs/STREAMING.md §5 Decision #20), and purges any torrents left
// over in category from a prior crash/restart (Decision #10), which is itself
// fatal on failure (Decision #16) to keep "clean slate on startup" an actual
// guarantee.
func NewQBitClient(host, user, pass, remoteRoot, downloadDir, category string, pollInterval, idleTimeout time.Duration) (TorrentClient, error) {
	api := qbt.NewClient(qbt.Config{Host: host, Username: user, Password: pass, Timeout: 10})
	return newQBitClientWithAPI(api, remoteRoot, downloadDir, category, pollInterval, idleTimeout)
}

// newQBitClientWithAPI holds the fail-fast startup logic (login, path
// validation, category purge) against the qbtAPI interface rather than the
// concrete *qbt.Client, so it's unit-testable with a fake — no live qBittorrent
// instance needed.
func newQBitClientWithAPI(api qbtAPI, remoteRoot, downloadDir, category string, pollInterval, idleTimeout time.Duration) (TorrentClient, error) {
	loginCtx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	if err := api.LoginCtx(loginCtx); err != nil {
		return nil, fmt.Errorf("qbt: login: %w", err)
	}

	info, err := os.Stat(downloadDir)
	if err != nil {
		return nil, fmt.Errorf("qbt: STREAM_QBIT_DOWNLOAD_DIR %q: %w", downloadDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("qbt: STREAM_QBIT_DOWNLOAD_DIR %q is not a directory", downloadDir)
	}

	if err := purgeCategory(api, category); err != nil {
		return nil, fmt.Errorf("qbt: startup purge of category %q failed: %w", category, err)
	}

	return &qbtClient{
		api:          api,
		remoteRoot:   remoteRoot,
		downloadDir:  downloadDir,
		category:     category,
		pollInterval: pollInterval,
		idleTimeout:  idleTimeout,
	}, nil
}

// purgeCategory deletes (with data) every torrent tagged with category — the
// qBittorrent-engine equivalent of anacrolix's WipeDownloadDir() clean slate.
func purgeCategory(api qbtAPI, category string) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	torrents, err := api.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{Category: category})
	if err != nil {
		return err
	}
	if len(torrents) == 0 {
		return nil
	}
	hashes := make([]string, len(torrents))
	for i, t := range torrents {
		hashes[i] = t.Hash
	}
	delCtx, delCancel := context.WithTimeout(context.Background(), apiTimeout)
	defer delCancel()
	return api.DeleteTorrentsCtx(delCtx, hashes, true)
}

func (c *qbtClient) AddMagnet(uri string) (Torrent, error) {
	clean := sanitizeMagnet(uri)
	hash, err := parseMagnetInfohash(clean)
	if err != nil {
		return nil, err
	}

	opts := map[string]string{
		"category":           c.category,
		"sequentialDownload": "true",
		"firstLastPiecePrio": "true",
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	if _, err := c.api.AddTorrentFromUrlCtx(ctx, clean, opts); err != nil {
		return nil, fmt.Errorf("qbt: add torrent: %w", err)
	}

	t := &qbtTorrent{
		hash:         hash,
		api:          c.api,
		downloadDir:  c.downloadDir,
		remoteRoot:   c.remoteRoot,
		pollInterval: c.pollInterval,
		idleTimeout:  c.idleTimeout,
		stopCh:       make(chan struct{}),
		gotInfo:      make(chan struct{}),
		refcounts:    make(map[int]int),
	}
	go t.pollMetadata()
	return t, nil
}

func (c *qbtClient) Close() {}

// parseMagnetInfohash extracts the lowercase-hex SHA1 infohash from a magnet's
// xt=urn:btih:... parameter — hex (40 chars) as-is, base32 (32 chars) decoded.
// v2/btmh-only magnets aren't supported (same limitation qbt_peers.go already
// has). qBittorrent's AddTorrentFromUrl doesn't return the hash synchronously
// the way anacrolix's AddMagnet does, so this adapter must derive it itself.
func parseMagnetInfohash(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("qbt: invalid magnet uri: %w", err)
	}
	const prefix = "urn:btih:"
	for _, xt := range u.Query()["xt"] {
		if !strings.HasPrefix(xt, prefix) {
			continue
		}
		v := xt[len(prefix):]
		switch len(v) {
		case 40:
			return strings.ToLower(v), nil
		case 32:
			dec, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(v))
			if err != nil || len(dec) != 20 {
				return "", fmt.Errorf("qbt: invalid base32 btih %q", v)
			}
			return hex.EncodeToString(dec), nil
		}
	}
	return "", fmt.Errorf("qbt: magnet has no usable btih infohash")
}

// mapPath translates qBittorrent's own save-path root into the local filesystem
// root the streamer container sees for the same (bind-mounted) directory. A
// savePath not prefixed by remoteRoot is an explicit configuration error, not a
// silently wrong path (§5.8).
func mapPath(savePath, remoteRoot, downloadDir string) (string, error) {
	if remoteRoot == "" || downloadDir == "" {
		return "", fmt.Errorf("qbt: STREAM_QBIT_REMOTE_ROOT and STREAM_QBIT_DOWNLOAD_DIR must both be set")
	}
	if !strings.HasPrefix(savePath, remoteRoot) {
		return "", fmt.Errorf("qbt: save path %q is not under configured remote root %q", savePath, remoteRoot)
	}
	return filepath.Join(downloadDir, strings.TrimPrefix(savePath, remoteRoot)), nil
}

// --- qbtTorrent (Torrent + filePrioritizer) ---

type qbtTorrent struct {
	hash         string
	api          qbtAPI
	downloadDir  string
	remoteRoot   string
	pollInterval time.Duration
	idleTimeout  time.Duration

	stopCh   chan struct{}
	stopOnce sync.Once

	gotInfo     chan struct{}
	gotInfoOnce sync.Once

	mu             sync.Mutex
	name           string
	savePath       string
	pieceSize      int64
	filesCache     []TorrentFile
	prioritized    int
	hasPrioritized bool
	refcounts      map[int]int
}

// pollMetadata polls qBittorrent until the torrent's metadata (file list, piece
// size) is available, then zeroes every file's download priority (qBittorrent
// has no add-time per-file priority option, so this establishes "nothing
// downloads by default" as the baseline — PrioritizeFile then promotes exactly
// one file at a time, implementing "only the picked file downloads", Decision
// #6) and closes gotInfo. API errors are treated as transient and retried —
// awaitInfo's outer MetadataTimeout remains the sole give-up point (Decision
// #15). Exits early if Drop() closes stopCh first.
func (t *qbtTorrent) pollMetadata() {
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			props, err := t.api.GetTorrentPropertiesCtx(ctx, t.hash)
			cancel()
			if err != nil {
				log.Printf("streamer: qbt metadata poll hash=%.8s: %v (retrying)", t.hash, err)
				continue
			}
			if props.PiecesNum <= 0 {
				continue
			}

			ctx2, cancel2 := context.WithTimeout(context.Background(), apiTimeout)
			files, err := t.api.GetFilesInformationCtx(ctx2, t.hash)
			cancel2()
			if err != nil || files == nil || len(*files) == 0 {
				continue // metadata reported ready but files not available yet
			}

			ids := make([]string, len(*files))
			for i := range *files {
				ids[i] = strconv.Itoa(i)
			}
			ctx3, cancel3 := context.WithTimeout(context.Background(), apiTimeout)
			if err := t.api.SetFilePriorityCtx(ctx3, t.hash, strings.Join(ids, "|"), 0); err != nil {
				log.Printf("streamer: qbt zero file priorities hash=%.8s: %v", t.hash, err)
			}
			cancel3()

			t.mu.Lock()
			t.name = props.Name
			t.savePath = props.SavePath
			t.pieceSize = int64(props.PieceSize)
			t.filesCache = t.buildFiles(*files)
			t.mu.Unlock()

			t.gotInfoOnce.Do(func() { close(t.gotInfo) })
			return
		}
	}
}

// buildFiles maps qBittorrent's file list into []TorrentFile, computing each
// file's cumulative byte offset within the torrent (needed for the piece-aware
// reader's piece-index math, §5.8) from files ordered by their Index.
func (t *qbtTorrent) buildFiles(raw qbt.TorrentFiles) []TorrentFile {
	sorted := make([]qbt.TorrentFile, len(raw))
	copy(sorted, raw)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Index < sorted[j].Index })

	out := make([]TorrentFile, len(sorted))
	var offset int64
	for i, f := range sorted {
		out[i] = &qbtFile{
			hash:         t.hash,
			index:        f.Index,
			name:         f.Name,
			size:         f.Size,
			fileOffset:   offset,
			progress:     f.Progress,
			api:          t.api,
			downloadDir:  t.downloadDir,
			remoteRoot:   t.remoteRoot,
			pollInterval: t.pollInterval,
			torrent:      t,
		}
		offset += f.Size
	}
	return out
}

func (t *qbtTorrent) GotInfo() <-chan struct{} { return t.gotInfo }
func (t *qbtTorrent) InfoHash() string         { return t.hash }

func (t *qbtTorrent) Name() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.name
}

func (t *qbtTorrent) Files() []TorrentFile {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.filesCache
}

func (t *qbtTorrent) Stats() TorrentStat {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	props, err := t.api.GetTorrentPropertiesCtx(ctx, t.hash)
	if err != nil {
		return TorrentStat{}
	}
	return TorrentStat{ConnectedSeeders: props.Seeds, ActivePeers: props.Peers, TotalPeers: props.PeersTotal}
}

// AddTrackers is best-effort (log + ignore errors), matching the existing
// tracker-augmentation posture (Decision #9).
func (t *qbtTorrent) AddTrackers(tiers [][]string) {
	var urls []string
	for _, tier := range tiers {
		urls = append(urls, tier...)
	}
	if len(urls) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	if err := t.api.AddTrackersCtx(ctx, t.hash, strings.Join(urls, "\n")); err != nil {
		log.Printf("streamer: qbt add trackers hash=%.8s: %v", t.hash, err)
	}
}

func (t *qbtTorrent) Drop() error {
	t.stopOnce.Do(func() { close(t.stopCh) })
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	return t.api.DeleteTorrentsCtx(ctx, []string{t.hash}, true)
}

// PrioritizeFile implements filePrioritizer: it promotes index to normal
// priority and demotes whichever file was previously prioritized, so only one
// file downloads at a time (Decision #6). A repeat call for the same index is a
// no-op, avoiding an API call on every byte-range request.
func (t *qbtTorrent) PrioritizeFile(index int) {
	t.mu.Lock()
	prev, hadPrev := t.prioritized, t.hasPrioritized
	if hadPrev && prev == index {
		t.mu.Unlock()
		return
	}
	t.prioritized, t.hasPrioritized = index, true
	t.mu.Unlock()

	func() {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		if err := t.api.SetFilePriorityCtx(ctx, t.hash, strconv.Itoa(index), 1); err != nil {
			log.Printf("streamer: qbt set priority hash=%.8s index=%d: %v", t.hash, index, err)
		}
	}()

	if hadPrev && prev != index {
		t.tryDemote(prev)
	}
}

// tryDemote demotes index to skip (priority 0) immediately if no reader
// currently holds it open; otherwise it defers demotion until the reader
// closes (releaseReader) or a bounded grace period (idleTimeout) elapses,
// whichever first — preventing a file switch from starving an in-flight read
// (which would otherwise hang forever, since there's no read-cancellation) while
// still bounding the worst case instead of leaving it demoted forever
// (Decision #14 + #18).
func (t *qbtTorrent) tryDemote(index int) {
	t.mu.Lock()
	refs := t.refcounts[index]
	stillStale := t.prioritized != index
	t.mu.Unlock()
	if !stillStale {
		return
	}
	if refs == 0 {
		t.demoteNow(index)
		return
	}

	go func() {
		timer := time.NewTimer(t.idleTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			t.mu.Lock()
			refs := t.refcounts[index]
			stillStale := t.prioritized != index
			t.mu.Unlock()
			if stillStale && refs > 0 {
				log.Printf("streamer: qbt hash=%.8s force-demoting file %d after %s grace period (reader still open)", t.hash, index, t.idleTimeout)
			}
			if stillStale {
				t.demoteNow(index)
			}
		case <-t.stopCh:
		}
	}()
}

// demoteNow issues the demote call and then self-corrects if index was
// re-promoted while the call was in flight: the staleness check callers do
// beforehand is inherently TOCTOU (a concurrent PrioritizeFile(index) can land
// between that check and this network call completing), and because
// PrioritizeFile dedups repeat picks of the same index, a demote landing after
// a re-promotion would otherwise strand the file at priority 0 with no future
// call to fix it. Re-checking afterward and re-promoting if needed closes that
// window instead of just narrowing it.
func (t *qbtTorrent) demoteNow(index int) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	if err := t.api.SetFilePriorityCtx(ctx, t.hash, strconv.Itoa(index), 0); err != nil {
		log.Printf("streamer: qbt demote hash=%.8s index=%d: %v", t.hash, index, err)
	}
	cancel()

	t.mu.Lock()
	reclaimed := t.prioritized == index
	t.mu.Unlock()
	if !reclaimed {
		return
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel2()
	if err := t.api.SetFilePriorityCtx(ctx2, t.hash, strconv.Itoa(index), 1); err != nil {
		log.Printf("streamer: qbt re-promote hash=%.8s index=%d after racing demote: %v", t.hash, index, err)
	}
}

func (t *qbtTorrent) acquireReader(index int) {
	t.mu.Lock()
	t.refcounts[index]++
	t.mu.Unlock()
}

// releaseReader decrements index's open-reader count and, if it reaches zero
// while index is no longer the current pick (a deferred demotion was waiting on
// this), demotes it immediately rather than waiting out the rest of the grace
// period.
func (t *qbtTorrent) releaseReader(index int) {
	t.mu.Lock()
	t.refcounts[index]--
	refs := t.refcounts[index]
	stale := t.prioritized != index
	t.mu.Unlock()
	if refs == 0 && stale {
		t.demoteNow(index)
	}
}

// --- qbtFile (TorrentFile) ---

type qbtFile struct {
	hash         string
	index        int
	name         string
	size         int64
	fileOffset   int64
	progress     float32
	api          qbtAPI
	downloadDir  string
	remoteRoot   string
	pollInterval time.Duration
	torrent      *qbtTorrent
}

func (f *qbtFile) Path() string  { return f.name }
func (f *qbtFile) Length() int64 { return f.size }
func (f *qbtFile) BytesCompleted() int64 {
	return int64(float64(f.progress) * float64(f.size))
}

func (f *qbtFile) NewReader() Reader {
	f.torrent.mu.Lock()
	savePath, pieceSize := f.torrent.savePath, f.torrent.pieceSize
	f.torrent.mu.Unlock()

	localPath, pathErr := mapPath(savePath, f.remoteRoot, f.downloadDir)
	if pathErr == nil {
		localPath = filepath.Join(localPath, f.name)
	}

	f.torrent.acquireReader(f.index)
	return &qbtReader{
		hash:           f.hash,
		index:          f.index,
		fileOffset:     f.fileOffset,
		size:           f.size,
		pieceSize:      pieceSize,
		localPath:      localPath,
		pathErr:        pathErr,
		api:            f.api,
		pollInterval:   f.pollInterval,
		torrent:        f.torrent,
		confirmedPiece: -1,
	}
}
