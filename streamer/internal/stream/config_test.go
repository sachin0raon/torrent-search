package stream

import "testing"

func TestTrackerURLs(t *testing.T) {
	t.Run("empty falls back to defaults", func(t *testing.T) {
		t.Setenv("STREAM_TRACKERS_URLS", "")
		if got := trackerURLs(); len(got) != len(DefaultTrackerURLs) {
			t.Errorf("empty should yield %d defaults, got %d", len(DefaultTrackerURLs), len(got))
		}
	})

	t.Run("none disables", func(t *testing.T) {
		for _, v := range []string{"none", "NONE", "off", "disabled"} {
			t.Setenv("STREAM_TRACKERS_URLS", v)
			if got := trackerURLs(); got != nil {
				t.Errorf("%q should disable trackers, got %v", v, got)
			}
		}
	})

	t.Run("explicit list is parsed and trimmed", func(t *testing.T) {
		t.Setenv("STREAM_TRACKERS_URLS", " udp://a:80 , http://b/announce ")
		got := trackerURLs()
		if len(got) != 2 || got[0] != "udp://a:80" || got[1] != "http://b/announce" {
			t.Errorf("unexpected list: %v", got)
		}
	})
}
