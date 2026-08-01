package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// ErrLocalFileMissing is returned when qBittorrent reports a piece as fully
// downloaded but the corresponding local file still can't be opened — a
// path-mapping misconfiguration (Decision #20 in docs/STREAMING.md §5), not a
// timing issue, so it's surfaced immediately rather than retried forever. The
// "file doesn't exist yet" case (recoverable) is handled separately: Read never
// reaches the point of opening the file until the relevant piece is confirmed
// downloaded.
var ErrLocalFileMissing = errors.New("qbt: piece reported downloaded but local file is missing (check STREAM_QBIT_REMOTE_ROOT / STREAM_QBIT_DOWNLOAD_DIR)")

// ErrTorrentGone is returned when the underlying qBittorrent torrent no longer
// exists — e.g. deleted directly via qBittorrent's own UI, out-of-band from
// this app. Without this check, a gone torrent is indistinguishable from a
// slow one (qBittorrent's piece-states endpoint doesn't reliably signal "not
// found" the way its properties endpoint does), and the wait loop in Read
// would retry forever instead of ever resolving.
var ErrTorrentGone = errors.New("qbt: torrent no longer exists in qBittorrent")

// existenceCheckEvery bounds how often Read's wait loop verifies the torrent
// still exists (a GetTorrentPropertiesCtx call), independent of pieceReady's
// own success/failure — catches an out-of-band deletion within a bounded
// number of poll iterations rather than looping indefinitely.
const existenceCheckEvery = 10

// qbtReader is the piece-aware Reader for the qBittorrent engine: it reads
// directly off disk from qBittorrent's download directory, blocking until the
// piece covering the current read position is confirmed downloaded (§5.8).
type qbtReader struct {
	hash       string
	index      int
	fileOffset int64 // this file's byte offset within the whole torrent
	size       int64
	pieceSize  int64
	pos        int64

	// localPaths are candidate on-disk locations, tried in order (see
	// qbtFile.NewReader — typically qBittorrent's download_path, i.e. its
	// separate incomplete-files location if configured, before save_path,
	// its final destination). Whichever is found to actually exist is used
	// for the rest of this reader's lifetime.
	localPaths []string
	pathErr    error // set at construction if no candidate path could be mapped at all

	api          qbtAPI
	pollInterval time.Duration
	torrent      *qbtTorrent

	f      *os.File
	closed bool

	// confirmedPiece caches the last piece index observed as fully downloaded.
	// A piece never transitions back to not-downloaded, so once confirmed it
	// can be trusted indefinitely — this avoids a network round-trip to
	// GetTorrentPieceStatesCtx on every single Read() call. Reads normally stay
	// within the same piece for many consecutive calls (piece size is typically
	// far larger than an HTTP chunk-read size), so this eliminates the vast
	// majority of otherwise-redundant polling once a region is downloaded.
	confirmedPiece int // -1 = none yet
}

// SetReadahead is a no-op: sequential-download + first/last-piece-priority (set
// at add-time) already drives qBittorrent's own prefetch; there's no per-read
// lever to pull here the way anacrolix's reader has.
func (r *qbtReader) SetReadahead(int64) {}

func (r *qbtReader) Read(p []byte) (int, error) {
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
		return 0, fmt.Errorf("qbt: unknown piece size for hash=%.8s", r.hash)
	}

	globalOffset := r.fileOffset + r.pos
	pieceIndex := int(globalOffset / r.pieceSize)
	pieceEndLocal := int64(pieceIndex+1)*r.pieceSize - r.fileOffset

	// Clamp so the returned slice never crosses into an unconfirmed piece —
	// this is what guarantees no zero-filled preallocated bytes are ever served
	// as real data. pieceEndLocal - r.pos is mathematically always > 0 here (pos
	// is always strictly within [pieceIndex*pieceSize, pieceEndLocal) of its
	// piece), so n cannot legitimately end up <= 0 given len(p) > 0 above — but
	// clamp defensively rather than ever slicing p[:n] out of bounds.
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
			ready, err := r.pieceReady(pieceIndex)
			if err == nil && ready {
				break
			}
			// Neither an error nor "not ready" is distinguishable on its own
			// from "this torrent was deleted out-of-band" (e.g. directly via
			// qBittorrent's own UI, not through this app) — qBittorrent's
			// piece-states endpoint doesn't reliably signal "not found" the
			// way its properties endpoint does. Left unchecked, that would
			// retry forever, since a gone torrent never becomes "ready."
			// Periodically (not every iteration, to bound the extra API
			// calls) verify the torrent still exists and bail out if not.
			if iter > 0 && iter%existenceCheckEvery == 0 && !r.torrentExists() {
				// Let Manager clean up its own bookkeeping immediately, so a
				// subsequent request for this session gets the existing clean
				// 410 Gone / "Restart stream" flow instead of repeating this
				// same detect-then-fail cycle.
				if r.torrent != nil {
					r.torrent.notifyGone()
				}
				return 0, ErrTorrentGone
			}
			time.Sleep(r.pollInterval)
		}
		r.confirmedPiece = pieceIndex
	}

	if r.f == nil {
		f, err := r.openFirstExisting()
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

// torrentExists checks whether the torrent still exists in qBittorrent *and*
// still has its files, returning false for either a definitive "not found"
// (deleted entirely) or qBittorrent's own missingFiles state (the torrent
// entry persists, but qBittorrent itself has determined its files are gone
// from disk — e.g. manual deletion outside qBittorrent, an unmounted volume,
// or a failed recheck). Using qBittorrent's own signal here rather than an
// independent disk check on our side avoids reintroducing the false-positive
// risk ErrLocalFileMissing's design already had to guard against: a file not
// existing *yet* is normal mid-download, and only qBittorrent's own piece/file
// tracking can reliably distinguish that from genuinely gone. Any error making
// this check (network blip, timeout) is treated as inconclusive — assume the
// torrent still exists — so a transient failure can never produce a
// false-positive "gone" verdict and abort a stream that would have recovered.
func (r *qbtReader) torrentExists() bool {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	torrents, err := r.api.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{Hashes: []string{r.hash}})
	if err != nil {
		return true
	}
	if len(torrents) == 0 {
		return false
	}
	return torrents[0].State != qbt.TorrentStateMissingFiles
}

// openFirstExisting tries each candidate path in order, returning the first
// one that opens successfully. If none exist, the piece covering the current
// position is already confirmed downloaded (Read only calls this after that),
// so a missing file at every candidate is unrecoverable — ErrLocalFileMissing,
// not a timing issue.
func (r *qbtReader) openFirstExisting() (*os.File, error) {
	for _, path := range r.localPaths {
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, ErrLocalFileMissing
}

// pieceReady reports whether pieceIndex is fully downloaded. It fetches the full
// piece-state array on every call — qBittorrent exposes no per-piece endpoint.
func (r *qbtReader) pieceReady(pieceIndex int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	states, err := r.api.GetTorrentPieceStatesCtx(ctx, r.hash)
	if err != nil {
		return false, err
	}
	// Bounds safety (Decision #13): qBittorrent can report metadata as ready
	// before its piece-map is fully populated — never index out of range.
	if pieceIndex < 0 || pieceIndex >= len(states) {
		return false, nil
	}
	return states[pieceIndex] == qbt.PieceStateAlreadyDownloaded, nil
}

func (r *qbtReader) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = r.pos + offset
	case io.SeekEnd:
		newPos = r.size + offset
	default:
		return 0, fmt.Errorf("qbt: invalid whence %d", whence)
	}
	if newPos < 0 {
		return 0, fmt.Errorf("qbt: negative seek position %d", newPos)
	}
	r.pos = newPos
	return r.pos, nil
}

func (r *qbtReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	var err error
	if r.f != nil {
		err = r.f.Close()
	}
	if r.torrent != nil {
		r.torrent.releaseReader(r.index)
	}
	return err
}
