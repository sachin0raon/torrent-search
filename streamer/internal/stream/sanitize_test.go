package stream

import (
	"strings"
	"testing"
)

func TestSanitizeMagnet(t *testing.T) {
	t.Run("tracker: prefix is unwrapped", func(t *testing.T) {
		// t-ru.org style: each tracker URL is prefixed with "tracker:"
		m := "magnet:?xt=urn:btih:abc123" +
			"&tr=tracker%3Ahttp%3A%2F%2Fbt.t-ru.org%2Fann%3Fmagnet" +
			"&tr=tracker%3Audp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce"
		got := sanitizeMagnet(m)
		if strings.Contains(got, "tracker%3A") || strings.Contains(got, "tracker:") {
			t.Errorf("tracker: prefix not stripped: %s", got)
		}
		if !strings.Contains(got, "bt.t-ru.org") {
			t.Errorf("http tracker lost after stripping prefix: %s", got)
		}
		if !strings.Contains(got, "opentrackr.org") {
			t.Errorf("udp tracker lost after stripping prefix: %s", got)
		}
	})

	t.Run("unsupported scheme dropped", func(t *testing.T) {
		m := "magnet:?xt=urn:btih:abc123" +
			"&tr=wss%3A%2F%2Ftracker.openwebtorrent.com" +
			"&tr=http%3A%2F%2Fgood.tracker.org%3A80%2Fannounce"
		got := sanitizeMagnet(m)
		if strings.Contains(got, "openwebtorrent") {
			t.Errorf("wss:// tracker should be dropped: %s", got)
		}
		if !strings.Contains(got, "good.tracker.org") {
			t.Errorf("http tracker should be kept: %s", got)
		}
	})

	t.Run("clean magnet returned unchanged", func(t *testing.T) {
		m := "magnet:?xt=urn:btih:abc123&tr=udp%3A%2F%2Ftracker.example.com%3A6969"
		got := sanitizeMagnet(m)
		if got != m {
			t.Errorf("clean magnet modified: got %s", got)
		}
	})

	t.Run("no trackers unchanged", func(t *testing.T) {
		m := "magnet:?xt=urn:btih:abc123&dn=Movie"
		if got := sanitizeMagnet(m); got != m {
			t.Errorf("no-tracker magnet modified: got %s", got)
		}
	})
}
