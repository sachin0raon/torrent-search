package stream

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// serveRange serves r from the given ReadSeeker with full HTTP range-request
// support (Range, 206 Partial Content, Accept-Ranges, Content-Length).
//
// It is a deliberate replacement for http.ServeContent for the qBittorrent
// piece-aware readers (qbtReader / downloadReader). http.ServeContent calls
// Seek(0, io.SeekEnd) internally to determine the file size before writing any
// bytes — for a piece-aware reader, that Seek moves r.pos to the end of the
// file, so the first Read blocks until the *last* piece of the torrent is fully
// downloaded and hash-verified, which can take minutes into a fresh stream even
// when the first piece is long ready (docs/STREAMING.md §5.8 known limitation).
//
// serveRange accepts the size the caller already knows (from FileInfo / the
// qBittorrent files API) and never issues a SeekEnd, so the player receives its
// first bytes as soon as piece 0 (the beginning of the file) is available.
//
// Range parsing covers the common "bytes=start-end" subset that video players
// emit. Multi-part ranges are not supported (returns 200 for those, matching
// http.ServeContent's documented behaviour for that edge case). The caller is
// responsible for setting Content-Type and Content-Disposition before calling.
func serveRange(w http.ResponseWriter, r *http.Request, rs io.ReadSeeker, size int64) {
	w.Header().Set("Accept-Ranges", "bytes")

	rangeHdr := r.Header.Get("Range")
	if rangeHdr == "" {
		// Full-file request: simple streaming response.
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, rs)
		return
	}

	// Parse "bytes=start-end". We only handle a single range; multi-part is
	// rare enough (no real player does it for video) that falling back to a
	// full 200 is acceptable.
	start, end, ok := parseSingleByteRange(rangeHdr, size)
	if !ok {
		// Unsatisfiable range — tell the client the actual extent.
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "Range Not Satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Seek to the requested start before writing anything. For the
	// piece-aware reader, Seek just updates r.pos (no I/O, no blocking).
	if _, err := rs.Seek(start, io.SeekStart); err != nil {
		http.Error(w, "seek error", http.StatusInternalServerError)
		return
	}

	length := end - start + 1
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = io.CopyN(w, rs, length)
}

// parseSingleByteRange parses a "bytes=start-end" Range header and clamps
// both ends to [0, size-1]. Returns ok=false for unsatisfiable or
// multi-part ranges.
func parseSingleByteRange(hdr string, size int64) (start, end int64, ok bool) {
	const prefix = "bytes="
	if !strings.HasPrefix(hdr, prefix) {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(hdr, prefix)

	// Multi-part ranges ("bytes=0-499,1000-1499") — not supported.
	if strings.Contains(spec, ",") {
		return 0, 0, false
	}

	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	startStr, endStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])

	if size <= 0 {
		return 0, 0, false
	}

	switch {
	case startStr == "" && endStr != "":
		// Suffix range: "bytes=-N" → last N bytes.
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		start = size - n
		if start < 0 {
			start = 0
		}
		end = size - 1

	case startStr != "" && endStr == "":
		// Open-ended range: "bytes=N-" → from N to EOF.
		var err error
		start, err = strconv.ParseInt(startStr, 10, 64)
		if err != nil || start < 0 {
			return 0, 0, false
		}
		end = size - 1

	default:
		// Explicit range: "bytes=start-end".
		var err error
		start, err = strconv.ParseInt(startStr, 10, 64)
		if err != nil || start < 0 {
			return 0, 0, false
		}
		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
	}

	if start >= size {
		return 0, 0, false // unsatisfiable
	}
	if end >= size {
		end = size - 1 // clamp to file boundary
	}
	return start, end, true
}
