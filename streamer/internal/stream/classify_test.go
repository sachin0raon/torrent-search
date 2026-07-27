package stream

import "testing"

func TestIsStreamable(t *testing.T) {
	cases := map[string]bool{
		"Movie/film.mkv":       true,
		"film.MP4":             true,
		"clip.webm":            true,
		"show.s01e01.avi":      true,
		"stream.ts":            true,
		"readme.txt":           false,
		"cover.jpg":            false,
		"noextension":          false,
		"archive.mkv.zip":      false,
		"nested/dir/movie.mov": true,
	}
	for path, want := range cases {
		if got := isStreamable(path); got != want {
			t.Errorf("isStreamable(%q) = %v, want %v", path, got, want)
		}
	}
}
