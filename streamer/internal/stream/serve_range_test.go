package stream

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// staticReader is an in-memory ReadSeeker that records Seek calls so tests
// can assert that no SeekEnd was issued.
type staticReader struct {
	data    []byte
	pos     int64
	seeks   []seekCall
}

type seekCall struct {
	offset int64
	whence int
}

func (s *staticReader) Read(p []byte) (int, error) {
	if s.pos >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.pos:])
	s.pos += int64(n)
	return n, nil
}

func (s *staticReader) Seek(offset int64, whence int) (int64, error) {
	s.seeks = append(s.seeks, seekCall{offset, whence})
	switch whence {
	case io.SeekStart:
		s.pos = offset
	case io.SeekCurrent:
		s.pos += offset
	case io.SeekEnd:
		s.pos = int64(len(s.data)) + offset
	}
	return s.pos, nil
}

func newStaticReader(content string) *staticReader {
	return &staticReader{data: []byte(content)}
}

// ---- parseSingleByteRange unit tests ----

func TestParseSingleByteRange(t *testing.T) {
	size := int64(1000)
	cases := []struct {
		hdr          string
		wantOk       bool
		wantStart    int64
		wantEnd      int64
	}{
		// Explicit range
		{"bytes=0-499", true, 0, 499},
		{"bytes=500-999", true, 500, 999},
		// Open-ended: from N to EOF
		{"bytes=500-", true, 500, 999},
		{"bytes=0-", true, 0, 999},
		// Suffix: last N bytes
		{"bytes=-100", true, 900, 999},
		// Clamp end to size-1
		{"bytes=0-9999", true, 0, 999},
		// Single byte
		{"bytes=0-0", true, 0, 0},
		// Unsatisfiable: start >= size
		{"bytes=1000-1099", false, 0, 0},
		// Unsatisfiable: start > end
		{"bytes=500-100", false, 0, 0},
		// Bad prefix
		{"items=0-100", false, 0, 0},
		// Multi-part — not supported
		{"bytes=0-499,500-999", false, 0, 0},
		// Empty spec
		{"bytes=", false, 0, 0},
	}
	for _, tc := range cases {
		start, end, ok := parseSingleByteRange(tc.hdr, size)
		if ok != tc.wantOk {
			t.Errorf("parseSingleByteRange(%q, %d): ok=%v want %v", tc.hdr, size, ok, tc.wantOk)
			continue
		}
		if ok && (start != tc.wantStart || end != tc.wantEnd) {
			t.Errorf("parseSingleByteRange(%q, %d): got [%d,%d] want [%d,%d]",
				tc.hdr, size, start, end, tc.wantStart, tc.wantEnd)
		}
	}
}

// ---- serveRange integration tests ----

// serveRange must NEVER issue a Seek(0, io.SeekEnd) — that's the whole point.
// An http.ServeContent call would issue one to discover the file size.
func TestServeRange_NoSeekEnd(t *testing.T) {
	content := strings.Repeat("A", 1024)
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/x/0/file.mkv", nil)

	serveRange(w, req, rs, int64(len(content)))

	for _, s := range rs.seeks {
		if s.whence == io.SeekEnd {
			t.Errorf("serveRange issued Seek(%d, SeekEnd) — this blocks the piece-aware reader", s.offset)
		}
	}
}

func TestServeRange_FullFile(t *testing.T) {
	content := "hello world"
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/x/0/file.mkv", nil)

	serveRange(w, req, rs, int64(len(content)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != content {
		t.Errorf("body = %q, want %q", got, content)
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("missing Accept-Ranges: bytes")
	}
}

func TestServeRange_ExplicitRange(t *testing.T) {
	content := "0123456789"
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/x/0/file.mkv", nil)
	req.Header.Set("Range", "bytes=2-5")

	serveRange(w, req, rs, int64(len(content)))

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if got := w.Body.String(); got != "2345" {
		t.Errorf("body = %q, want %q", got, "2345")
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Errorf("Content-Range = %q, want \"bytes 2-5/10\"", got)
	}
}

func TestServeRange_OpenEndedRange(t *testing.T) {
	content := "0123456789"
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=7-")

	serveRange(w, req, rs, int64(len(content)))

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if got := w.Body.String(); got != "789" {
		t.Errorf("body = %q, want %q", got, "789")
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 7-9/10" {
		t.Errorf("Content-Range = %q, want %q", got, "bytes 7-9/10")
	}
}

func TestServeRange_SuffixRange(t *testing.T) {
	content := "0123456789"
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=-3")

	serveRange(w, req, rs, int64(len(content)))

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if got := w.Body.String(); got != "789" {
		t.Errorf("body = %q, want %q", got, "789")
	}
}

func TestServeRange_UnsatisfiableRange(t *testing.T) {
	content := "hello"
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=100-200") // past EOF

	serveRange(w, req, rs, int64(len(content)))

	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", w.Code)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes */5" {
		t.Errorf("Content-Range = %q, want \"bytes */5\"", got)
	}
}

func TestServeRange_ClampedEnd(t *testing.T) {
	// "bytes=0-9999" with a 10-byte file: should clamp to bytes=0-9 and return 200-ok (full file)
	// but as a 206 since Range header was present.
	content := "0123456789"
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=0-9999")

	serveRange(w, req, rs, int64(len(content)))

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	// Should return all 10 bytes
	if got := w.Body.Bytes(); !bytes.Equal(got, []byte(content)) {
		t.Errorf("body = %q, want %q", got, content)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 0-9/10" {
		t.Errorf("Content-Range = %q, want \"bytes 0-9/10\"", got)
	}
}

func TestServeRange_ContentLengthSet(t *testing.T) {
	content := "abcdefgh"
	rs := newStaticReader(content)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Range", "bytes=2-5")

	serveRange(w, req, rs, int64(len(content)))

	if got := w.Header().Get("Content-Length"); got != "4" {
		t.Errorf("Content-Length = %q, want \"4\"", got)
	}
}
