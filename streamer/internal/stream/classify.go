package stream

import (
	"path"
	"strings"
)

// videoExtensions is the allowlist of file extensions we offer to stream.
var videoExtensions = map[string]struct{}{
	".mp4":  {},
	".mkv":  {},
	".avi":  {},
	".mov":  {},
	".m4v":  {},
	".webm": {},
	".ts":   {},
}

// isStreamable reports whether a file path looks like a playable video by its
// extension.
func isStreamable(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	_, ok := videoExtensions[ext]
	return ok
}
