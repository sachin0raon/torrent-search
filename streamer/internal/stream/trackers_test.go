package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseTrackers(t *testing.T) {
	in := strings.Join([]string{
		"udp://tracker.one:1337/announce",
		"",
		"# a comment",
		"  http://tracker.two/announce  ",
		"not-a-url",
		"wss://tracker.three:443",
	}, "\n")
	got, err := parseTrackers(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"udp://tracker.one:1337/announce",
		"http://tracker.two/announce",
		"wss://tracker.three:443",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestTrackerSource_FetchMergesAndDedups(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("udp://a:80\nudp://shared:80\n"))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("udp://shared:80\nhttp://b/announce\n"))
	}))
	defer srvB.Close()

	ts := NewTrackerSource([]string{srvA.URL, srvB.URL}, 5*time.Second)
	if err := ts.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if ts.Count() != 3 {
		t.Fatalf("expected 3 deduped trackers, got %d", ts.Count())
	}
	tiers := ts.Tiers()
	if len(tiers) != 3 || len(tiers[0]) != 1 {
		t.Fatalf("expected one-tracker-per-tier, got %v", tiers)
	}
}

func TestTrackerSource_PartialFailureKeepsResults(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("udp://a:80\n"))
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	ts := NewTrackerSource([]string{ok.URL, bad.URL}, 5*time.Second)
	if err := ts.Fetch(context.Background()); err != nil {
		t.Fatalf("partial failure should not error: %v", err)
	}
	if ts.Count() != 1 {
		t.Errorf("expected 1 tracker from the healthy source, got %d", ts.Count())
	}
}

func TestTrackerSource_AllFailReturnsError(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()

	ts := NewTrackerSource([]string{bad.URL}, 5*time.Second)
	if err := ts.Fetch(context.Background()); err == nil {
		t.Error("expected error when all sources fail")
	}
}
