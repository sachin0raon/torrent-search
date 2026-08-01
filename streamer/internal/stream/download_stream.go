package stream

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// downloadReader is the piece-aware Reader for the download-manager's byte-
// serving endpoint (docs/STREAMING.md §6 Decision #23). It's a deliberately
// separate, smaller type from qbtReader — Download has no Manager/Session,
// refcount, or deferred-demotion concerns to hook into (file selection is
// additive, never exclusive — see DownloadManager.SelectFiles) — but it
// reuses the extracted helpers (candidatePaths, pieceReady, torrentAlive,
// openFirstExisting) so the underlying, production-hardened piece-wait and
// path-resolution logic isn't duplicated, only this thin orchestration
// around it.
type downloadReader struct {
	hash       string
	fileOffset int64 // this file's byte offset within the whole torrent
	size       int64
	pieceSize  int64
	pos        int64

	localPaths []string
	pathErr    error

	api          qbtAPI
	pollInterval time.Duration

	f      *os.File
	closed bool

	// confirmedPiece caches the last piece index observed as fully
	// downloaded — see qbtReader's identical field for the rationale.
	confirmedPiece int
}

// SetReadahead is a no-op for the same reason as qbtReader's: qBittorrent's
// own sequential-download + first/last-piece-priority options (set at
// add-time) already drive its prefetch.
func (r *downloadReader) SetReadahead(int64) {}

func (r *downloadReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.pathErr != nil {
		return 0, r.pathErr
	}
	if r.pos >= r.size {
		return 0, io.EOF
	}
	if r.pieceSize <= 0 {
		return 0, fmt.Errorf("download: unknown piece size for hash=%.8s", r.hash)
	}

	globalOffset := r.fileOffset + r.pos
	pieceIndex := int(globalOffset / r.pieceSize)
	pieceEndLocal := int64(pieceIndex+1)*r.pieceSize - r.fileOffset

	// Clamp so the returned slice never crosses into an unconfirmed piece —
	// identical guarantee to qbtReader.Read.
	n := int64(len(p))
	if rem := r.size - r.pos; rem < n {
		n = rem
	}
	if rem := pieceEndLocal - r.pos; rem < n {
		n = rem
	}
	if n <= 0 {
		return 0, nil
	}

	if pieceIndex != r.confirmedPiece {
		for iter := 0; ; iter++ {
			ready, err := pieceReady(r.api, r.hash, pieceIndex)
			if err == nil && ready {
				break
			}
			// Same out-of-band-deletion detection as qbtReader.Read: neither
			// an error nor "not ready" distinguishes a slow piece from a
			// torrent deleted directly via qBittorrent's own UI, so
			// periodically confirm the torrent is still alive.
			if iter > 0 && iter%existenceCheckEvery == 0 && !torrentAlive(r.api, r.hash) {
				return 0, ErrTorrentGone
			}
			time.Sleep(r.pollInterval)
		}
		r.confirmedPiece = pieceIndex
	}

	if r.f == nil {
		f, err := openFirstExisting(r.localPaths)
		if err != nil {
			return 0, err
		}
		r.f = f
	}

	got, err := r.f.ReadAt(p[:n], r.pos)
	r.pos += int64(got)
	if err != nil && err != io.EOF {
		return got, err
	}
	return got, nil
}

func (r *downloadReader) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = r.pos + offset
	case io.SeekEnd:
		newPos = r.size + offset
	default:
		return 0, fmt.Errorf("download: invalid whence %d", whence)
	}
	if newPos < 0 {
		return 0, fmt.Errorf("download: negative seek position %d", newPos)
	}
	r.pos = newPos
	return r.pos, nil
}

func (r *downloadReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.f != nil {
		return r.f.Close()
	}
	return nil
}

// OpenFile returns a fresh reader for one file of a download-manager
// torrent, resolving its current on-disk location and piece layout live from
// qBittorrent — there is no cached Session to serve from (§6.2 Assumption
// #2), so every call re-fetches properties and the file list.
func (m *DownloadManager) OpenFile(ctx context.Context, hash string, index int) (Reader, DownloadFileInfo, error) {
	if hash == "" {
		return nil, DownloadFileInfo{}, ErrDownloadNotFound
	}

	// Check existence via GetTorrentsCtx (the same reliable list-and-filter
	// pattern Get and torrentAlive already use) rather than trusting
	// GetTorrentPropertiesCtx's error alone: that call fails identically for
	// "torrent genuinely gone" and "qBittorrent had a transient blip," and
	// conflating the two would map a passing network hiccup to a 404 instead
	// of the 503 every other handler in this file uses for that case.
	existCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	torrents, err := m.api.GetTorrentsCtx(existCtx, qbt.TorrentFilterOptions{Hashes: []string{hash}})
	cancel()
	if err != nil {
		return nil, DownloadFileInfo{}, fmt.Errorf("download: check torrent exists: %w", err)
	}
	if len(torrents) == 0 {
		return nil, DownloadFileInfo{}, ErrDownloadNotFound
	}

	propsCtx, cancel2 := context.WithTimeout(ctx, apiTimeout)
	props, err := m.api.GetTorrentPropertiesCtx(propsCtx, hash)
	cancel2()
	if err != nil {
		return nil, DownloadFileInfo{}, fmt.Errorf("download: get properties: %w", err)
	}

	filesCtx, cancel3 := context.WithTimeout(ctx, apiTimeout)
	files, err := m.api.GetFilesInformationCtx(filesCtx, hash)
	cancel3()
	if err != nil {
		return nil, DownloadFileInfo{}, fmt.Errorf("download: get files: %w", err)
	}
	if files == nil {
		return nil, DownloadFileInfo{}, ErrFileIndex
	}

	sorted := make([]qbt.TorrentFile, len(*files))
	copy(sorted, *files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Index < sorted[j].Index })

	if index < 0 || index >= len(sorted) {
		return nil, DownloadFileInfo{}, ErrFileIndex
	}

	var offset int64
	for i := 0; i < index; i++ {
		offset += sorted[i].Size
	}
	target := sorted[index]

	candidates, pathErr := candidatePaths(m.remoteRoot, m.downloadDir, props.SavePath, props.DownloadPath, target.Name)

	r := &downloadReader{
		hash:           hash,
		fileOffset:     offset,
		size:           target.Size,
		pieceSize:      int64(props.PieceSize),
		localPaths:     candidates,
		pathErr:        pathErr,
		api:            m.api,
		pollInterval:   m.pollInterval,
		confirmedPiece: -1,
	}
	info := DownloadFileInfo{
		Index:      target.Index,
		Name:       target.Name,
		Size:       target.Size,
		Downloaded: int64(float64(target.Progress) * float64(target.Size)),
		Selected:   target.Priority > 0,
	}
	return r, info, nil
}
