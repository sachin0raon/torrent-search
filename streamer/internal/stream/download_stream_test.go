package stream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestDownloadManager_OpenFile_ComputesOffsetByIndex(t *testing.T) {
	fake := newFakeQbtAPI()
	downloadDir := t.TempDir()
	m := newTestDownloadManagerWithDirs(t, fake, "/data/downloads", downloadDir)

	movieDir := filepath.Join(downloadDir, "Movie")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "b.mp4"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	const hash = "aaaa"
	fake.torrents[hash] = qbt.Torrent{Hash: hash}
	fake.props[hash] = qbt.TorrentProperties{SavePath: "/data/downloads/Movie", PieceSize: 1024}
	fake.files[hash] = qbt.TorrentFiles{
		{Index: 0, Name: "a.mp4", Size: 100},
		{Index: 1, Name: "b.mp4", Size: 5},
	}
	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	r, info, err := m.OpenFile(context.Background(), hash, 1)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer r.Close()

	if info.Index != 1 || info.Name != "b.mp4" || info.Size != 5 {
		t.Errorf("unexpected info: %+v", info)
	}
	dr := r.(*downloadReader)
	if dr.fileOffset != 100 {
		t.Errorf("fileOffset = %d, want 100 (size of file 0)", dr.fileOffset)
	}

	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 5 || string(buf) != "world" {
		t.Errorf("Read = %d %q, want 5 bytes \"world\"", n, buf)
	}
}

func TestDownloadManager_OpenFile_IndexOutOfRange(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManagerWithDirs(t, fake, "/data/downloads", t.TempDir())

	const hash = "bbbb"
	fake.torrents[hash] = qbt.Torrent{Hash: hash}
	fake.props[hash] = qbt.TorrentProperties{SavePath: "/data/downloads/Movie", PieceSize: 1024}
	fake.files[hash] = qbt.TorrentFiles{{Index: 0, Name: "a.mp4", Size: 100}}

	_, _, err := m.OpenFile(context.Background(), hash, 5)
	if !errors.Is(err, ErrFileIndex) {
		t.Fatalf("expected ErrFileIndex, got %v", err)
	}
}

func TestDownloadManager_OpenFile_TorrentNotFound(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManagerWithDirs(t, fake, "/data/downloads", t.TempDir())
	// fake.torrents has no entry for "missing" — GetTorrentsCtx returns none.

	_, _, err := m.OpenFile(context.Background(), "missing", 0)
	if !errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("expected ErrDownloadNotFound, got %v", err)
	}
}

// TestDownloadManager_OpenFile_TransientErrorIsNotNotFound guards against
// conflating "torrent genuinely gone" with "qBittorrent had a blip": a
// GetTorrentPropertiesCtx failure on a torrent GetTorrentsCtx just confirmed
// exists must not surface as ErrDownloadNotFound (which the HTTP handler
// maps to a permanent-looking 404) — it should be a plain wrapped error
// (mapped to 503 instead), consistent with every other handler in this file.
func TestDownloadManager_OpenFile_TransientErrorIsNotNotFound(t *testing.T) {
	fake := newFakeQbtAPI()
	m := newTestDownloadManagerWithDirs(t, fake, "/data/downloads", t.TempDir())
	const hash = "eeee"
	fake.torrents[hash] = qbt.Torrent{Hash: hash} // confirmed to exist
	fake.propsErr = errors.New("qbittorrent unreachable")

	_, _, err := m.OpenFile(context.Background(), hash, 0)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("a transient properties error must not be reported as ErrDownloadNotFound, got %v", err)
	}
}

// TestDownloadReader_WaitsForPieceThenReads covers the piece-gating behavior
// shared with qbtReader (via the extracted pieceReady helper): a read must
// block until the covering piece is reported downloaded.
func TestDownloadReader_WaitsForPieceThenReads(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mp4"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeQbtAPI()
	// No piece states set yet — Read must wait.

	r := &downloadReader{
		hash: "cc", fileOffset: 0, size: 11, pieceSize: 1024,
		localPaths: []string{filepath.Join(dir, "a.mp4")},
		api:        fake, pollInterval: 2 * time.Millisecond, confirmedPiece: -1,
	}
	defer r.Close()

	done := make(chan struct{})
	var n int
	var readErr error
	buf := make([]byte, 11)
	go func() {
		n, readErr = r.Read(buf)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Read returned before the piece was marked downloaded")
	case <-time.After(20 * time.Millisecond):
	}

	fake.setPieceStates("cc", []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not unblock after the piece became ready")
	}
	if readErr != nil {
		t.Fatalf("Read: %v", readErr)
	}
	if n != 11 || string(buf) != "hello world" {
		t.Errorf("Read = %d %q", n, buf)
	}
}

// TestDownloadReader_TorrentGoneOutOfBand covers the same out-of-band
// deletion detection as qbtReader, via the shared torrentAlive helper.
func TestDownloadReader_TorrentGoneOutOfBand(t *testing.T) {
	fake := newFakeQbtAPI()
	// Piece states never become ready, and the torrent doesn't exist —
	// GetTorrentsCtx (via torrentAlive) returns no match.

	r := &downloadReader{
		hash: "dd", fileOffset: 0, size: 5, pieceSize: 1024,
		localPaths: []string{filepath.Join(t.TempDir(), "missing.mp4")},
		api:        fake, pollInterval: time.Millisecond, confirmedPiece: -1,
	}
	defer r.Close()

	buf := make([]byte, 5)
	_, err := r.Read(buf)
	if !errors.Is(err, ErrTorrentGone) {
		t.Fatalf("expected ErrTorrentGone, got %v", err)
	}
}

func newTestDownloadManagerWithDirs(t *testing.T, fake *fakeQbtAPI, remoteRoot, downloadDir string) *DownloadManager {
	t.Helper()
	m, err := newDownloadManagerWithAPI(fake, remoteRoot, downloadDir, "tsa-download", 2*time.Millisecond, time.Hour)
	if err != nil {
		t.Fatalf("newDownloadManagerWithAPI: %v", err)
	}
	return m
}
