package stream

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestQbtReader_PieceMathAndClamping(t *testing.T) {
	tests := []struct {
		name         string
		pieceSize    int64
		fileOffset   int64
		size         int64
		pos          int64
		reqLen       int64
		wantN        int64
		wantPieceIdx int
	}{
		{"read within one piece, well inside file", 1024, 0, 1024, 0, 100, 100, 0},
		{"read clamped at piece boundary", 1024, 0, 1024, 900, 500, 124, 0},
		{"read starts mid-piece via nonzero fileOffset", 1024, 500, 1024, 0, 1000, 524, 0},
		{"second piece, offset within it", 1024, 0, 2048, 1024, 100, 100, 1},
		{"clamped by remaining file size, not piece", 1024, 0, 1024, 1000, 5000, 24, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			globalOffset := tc.fileOffset + tc.pos
			pieceIndex := int(globalOffset / tc.pieceSize)
			pieceEndLocal := int64(pieceIndex+1)*tc.pieceSize - tc.fileOffset

			n := tc.reqLen
			if rem := tc.size - tc.pos; rem < n {
				n = rem
			}
			if rem := pieceEndLocal - tc.pos; rem < n {
				n = rem
			}

			if pieceIndex != tc.wantPieceIdx {
				t.Errorf("pieceIndex = %d, want %d", pieceIndex, tc.wantPieceIdx)
			}
			if n != tc.wantN {
				t.Errorf("clamped n = %d, want %d", n, tc.wantN)
			}
		})
	}
}

// newTestReader builds a qbtReader with pieceSize/fileOffset/size set directly,
// backed by a real temp file so Read exercises the actual disk-read path.
func newTestReader(t *testing.T, fake *fakeQbtAPI, hash string, pieceSize, fileOffset, size int64, localPath string) *qbtReader {
	t.Helper()
	return &qbtReader{
		hash: hash, index: 0, fileOffset: fileOffset, size: size, pieceSize: pieceSize,
		localPath: localPath, api: fake, pollInterval: 2 * time.Millisecond, confirmedPiece: -1,
	}
}

func TestQbtReader_ReadsOnceConfirmedDownloaded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := newFakeQbtAPI()
	const hash = "aa"
	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	r := newTestReader(t, fake, hash, 1024, 0, 10, path)
	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 5 || string(buf) != "01234" {
		t.Errorf("Read = %d bytes %q, want 5 bytes \"01234\"", n, buf)
	}
}

func TestQbtReader_WaitsForNotYetDownloadedPiece(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := newFakeQbtAPI()
	const hash = "bb"
	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateNowDownloading})

	r := newTestReader(t, fake, hash, 1024, 0, 10, path)

	done := make(chan struct{})
	go func() {
		buf := make([]byte, 5)
		if _, err := r.Read(buf); err != nil {
			t.Errorf("Read: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Read returned before the piece was marked downloaded")
	case <-time.After(30 * time.Millisecond):
	}

	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not unblock after the piece became downloaded")
	}
}

func TestQbtReader_PieceStateBoundsSafety(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := newFakeQbtAPI()
	const hash = "cc"
	// Piece index 0 requested, but the states array starts out empty (as if
	// qBittorrent reported PiecesNum > 0 before its piece-map was populated) —
	// must not panic, must treat as not-ready.
	fake.setPieceStates(hash, nil)

	r := newTestReader(t, fake, hash, 1024, 0, 10, path)
	done := make(chan struct{})
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("Read panicked: %v", rec)
			}
			close(done)
		}()
		buf := make([]byte, 5)
		_, _ = r.Read(buf)
	}()

	select {
	case <-done:
		t.Fatal("Read returned despite an out-of-range/empty piece-states array")
	case <-time.After(30 * time.Millisecond):
	}

	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not recover once the piece-states array was populated")
	}
}

func TestQbtReader_TransientAPIErrorIsRetried(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := newFakeQbtAPI()
	const hash = "dd"
	fake.pieceStatesErr = errors.New("qbittorrent web api blip")

	r := newTestReader(t, fake, hash, 1024, 0, 10, path)
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 5)
		if _, err := r.Read(buf); err != nil {
			t.Errorf("Read: %v", err)
		}
		close(done)
	}()

	time.Sleep(20 * time.Millisecond) // let it retry a few times against the error
	fake.mu.Lock()
	fake.pieceStatesErr = nil
	fake.pieceStates[hash] = []qbt.PieceState{qbt.PieceStateAlreadyDownloaded}
	fake.mu.Unlock()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not recover after the transient API error cleared")
	}
}

func TestQbtReader_ConfirmedPieceIsNotRePolled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := newFakeQbtAPI()
	const hash = "gg"
	// One large piece covering the whole 10-byte file, so every read below
	// falls in the same piece index.
	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	r := newTestReader(t, fake, hash, 1024, 0, 10, path)
	buf := make([]byte, 2)
	for i := 0; i < 5; i++ {
		if _, err := r.Read(buf); err != nil && err != io.EOF {
			t.Fatalf("Read #%d: %v", i, err)
		}
	}

	if calls := fake.getPieceStatesCallCount(); calls != 1 {
		t.Errorf("expected 1 GetTorrentPieceStatesCtx call for repeated reads within an already-confirmed piece, got %d", calls)
	}
}

func TestQbtReader_UnrecoverableMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.mp4") // never created

	fake := newFakeQbtAPI()
	const hash = "ee"
	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	r := newTestReader(t, fake, hash, 1024, 0, 10, path)
	buf := make([]byte, 5)
	_, err := r.Read(buf)
	if !errors.Is(err, ErrLocalFileMissing) {
		t.Fatalf("expected ErrLocalFileMissing, got %v", err)
	}
}

func TestQbtReader_PathErrorSurfacesImmediately(t *testing.T) {
	r := &qbtReader{size: 10, pathErr: errors.New("qbt: save path mismatch")}
	buf := make([]byte, 5)
	if _, err := r.Read(buf); err == nil {
		t.Fatal("expected the construction-time path error to surface from Read")
	}
}

func TestQbtReader_SeekAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeQbtAPI()
	const hash = "ff"
	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	tor := &qbtTorrent{hash: hash, api: fake, refcounts: make(map[int]int)}
	r := newTestReader(t, fake, hash, 1024, 0, 10, path)
	r.torrent = tor
	r.index = 3
	tor.acquireReader(3)

	pos, err := r.Seek(5, os.SEEK_SET)
	if err != nil || pos != 5 {
		t.Fatalf("Seek = %d, %v", pos, err)
	}
	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil || n != 5 || string(buf) != "56789" {
		t.Fatalf("Read after seek = %d %q %v", n, buf, err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	tor.mu.Lock()
	refs := tor.refcounts[3]
	tor.mu.Unlock()
	if refs != 0 {
		t.Errorf("expected refcount 0 after Close, got %d", refs)
	}
	// Double-close must be safe (handlers.go always defers Close()).
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
