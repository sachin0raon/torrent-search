package stream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, cfg Config, build func(string) *fakeTorrent) (*Manager, http.Handler) {
	t.Helper()
	mgr := NewManager(cfg, newFakeClient(build))
	t.Cleanup(mgr.Close)
	return mgr, NewHandler(mgr, cfg).Routes()
}

func TestCreateSession_OK(t *testing.T) {
	_, srv := newTestServer(t, testConfig(t), readyTorrent("Movie", &fakeFile{path: "Movie/a.mkv", data: []byte("data")}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/stream-api/sessions", strings.NewReader(`{"magnet":"magnet:?xt=urn:btih:AAA"}`))
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.SessionID == "" || !resp.Ready || len(resp.Files) != 1 || !resp.Files[0].Streamable {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateSession_BadRequest(t *testing.T) {
	_, srv := newTestServer(t, testConfig(t), readyTorrent("M"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/stream-api/sessions", strings.NewReader(`{"magnet":""}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateSession_Capacity(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxActive = 1
	mgr, srv := newTestServer(t, cfg, readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	if _, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", ""); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/stream-api/sessions", strings.NewReader(`{"magnet":"magnet:?xt=urn:btih:BBB"}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestGetSession_Gone(t *testing.T) {
	_, srv := newTestServer(t, testConfig(t), readyTorrent("M"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream-api/sessions/nope", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", rec.Code)
	}
}

func TestStreamFile_RangeAndFull(t *testing.T) {
	payload := []byte("0123456789")
	mgr, srv := newTestServer(t, testConfig(t), readyTorrent("Movie", &fakeFile{path: "Movie/a.mp4", data: payload}))
	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
	if err != nil {
		t.Fatal(err)
	}
	url := "/stream/" + s.ID + "/0/a.mp4"

	// Full request.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("full: expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("full: missing Accept-Ranges")
	}
	if got, _ := io.ReadAll(rec.Body); string(got) != "0123456789" {
		t.Errorf("full body = %q", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("content-type = %q", ct)
	}

	// Range request bytes=2-5.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Range", "bytes=2-5")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("range: expected 206, got %d", rec.Code)
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 2-5/10" {
		t.Errorf("content-range = %q", cr)
	}
	if got, _ := io.ReadAll(rec.Body); string(got) != "2345" {
		t.Errorf("range body = %q", got)
	}
}

func TestStreamFile_Gone(t *testing.T) {
	_, srv := newTestServer(t, testConfig(t), readyTorrent("M"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/missing/0/a.mp4", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", rec.Code)
	}
}

func TestDeleteSession(t *testing.T) {
	mgr, srv := newTestServer(t, testConfig(t), readyTorrent("M", &fakeFile{path: "a.mp4", data: []byte("x")}))
	s, err := mgr.AddSession(context.Background(), "magnet:?xt=urn:btih:AAA", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/stream-api/sessions/"+s.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/stream-api/sessions/"+s.ID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- docs/STREAMING.md §7.6: Active Streams panel endpoints ---

func newQBTestServer(t *testing.T, cfg Config, client *fakeQBClient) (*Manager, http.Handler) {
	t.Helper()
	mgr := NewManager(cfg, client)
	t.Cleanup(mgr.Close)
	return mgr, NewHandler(mgr, cfg).Routes()
}

func TestTorrentRoutes_404WhenEngineDoesNotSupportClientLister(t *testing.T) {
	// The plain fakeClient (anacrolix-like) doesn't implement clientLister, so
	// these routes must never be mounted at all — matching the existing
	// /download-api/* pattern for a disabled feature (docs/STREAMING.md §7.8).
	_, srv := newTestServer(t, testConfig(t), readyTorrent("M"))
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/stream-api/torrents", nil),
		httptest.NewRequest(http.MethodPost, "/stream-api/torrents/AAA/resume", nil),
		httptest.NewRequest(http.MethodDelete, "/stream-api/torrents/AAA", nil),
		httptest.NewRequest(http.MethodPost, "/stream-api/torrents/AAA/move-to-downloads", nil),
		httptest.NewRequest(http.MethodDelete, "/stream-api/torrents", nil),
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: expected 404, got %d", req.Method, req.URL.Path, rec.Code)
		}
	}
}

func TestListTorrents(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M"))
	client.list = []TorrentSummary{{Hash: "AAA", Name: "Movie", Paused: true}}
	_, srv := newQBTestServer(t, testQBConfig(t), client)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream-api/torrents", nil)
	req.Header.Set("X-Client-Id", "browser-1")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []TorrentSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Hash != "AAA" || !got[0].Paused {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestResumeTorrent(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M"))
	_, srv := newQBTestServer(t, testQBConfig(t), client)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/stream-api/torrents/AAA/resume", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(client.resumeCalls) != 1 || client.resumeCalls[0] != "AAA" {
		t.Errorf("expected resume delegated to the client, got %v", client.resumeCalls)
	}
}

func TestDeleteTorrent(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M"))
	_, srv := newQBTestServer(t, testQBConfig(t), client)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/stream-api/torrents/AAA", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(client.deleteCalls) != 1 || client.deleteCalls[0] != "AAA" {
		t.Errorf("expected delete delegated to the client, got %v", client.deleteCalls)
	}
}

func TestMoveTorrentToDownloads(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M"))
	cfg := testQBConfig(t)
	cfg.DownloadQBitCategory = "tsa-download"
	_, srv := newQBTestServer(t, cfg, client)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/stream-api/torrents/AAA/move-to-downloads", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(client.moveCalls) != 1 || client.moveCalls[0].category != "tsa-download" {
		t.Errorf("expected move-to-downloads delegated with the download category, got %v", client.moveCalls)
	}
}

func TestFlushTorrents(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M"))
	_, srv := newQBTestServer(t, testQBConfig(t), client)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/stream-api/torrents", nil)
	req.Header.Set("X-Client-Id", "browser-1")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestResumeTorrent_NotFound(t *testing.T) {
	client := newFakeQBClient(readyQBTorrent("M"))
	client.resumeErr = ErrTorrentNotFound
	_, srv := newQBTestServer(t, testQBConfig(t), client)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/stream-api/torrents/AAA/resume", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body = %s", rec.Code, rec.Body.String())
	}
}
