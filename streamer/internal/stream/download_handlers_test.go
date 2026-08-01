package stream

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// newDownloadTestServer wires a Handler with the download-manager feature
// enabled, mirroring newTestServer's shape in handlers_test.go but building a
// DownloadManager over a fakeQbtAPI instead of a Manager over a fake
// TorrentClient.
func newDownloadTestServer(t *testing.T, fake *fakeQbtAPI) (*DownloadManager, http.Handler) {
	t.Helper()
	dm, err := newDownloadManagerWithAPI(fake, "/data/downloads", t.TempDir(), "tsa-download", 2*time.Millisecond, time.Hour)
	if err != nil {
		t.Fatalf("newDownloadManagerWithAPI: %v", err)
	}
	h := NewHandler(nil, Config{MetadataTimeout: time.Second})
	h.SetDownloadManager(dm)
	return dm, h.Routes()
}

func TestDownloadStatus_Enabled(t *testing.T) {
	_, srv := newDownloadTestServer(t, newFakeQbtAPI())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download-api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body["enabled"] {
		t.Errorf("expected enabled=true, got %v", body)
	}
}

// TestDownloadStatus_AbsentWhenDisabled covers §6.8: routes must be genuinely
// absent (404 from the mux, not a handled "disabled" response) when
// DOWNLOAD_ENGINE is unset, since Handler.SetDownloadManager is simply never
// called in that case (see main.go's buildDownloadManager returning nil,nil).
func TestDownloadStatus_AbsentWhenDisabled(t *testing.T) {
	h := NewHandler(nil, Config{})
	srv := h.Routes()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download-api/status", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when download manager is unset, got %d", rec.Code)
	}
}

func TestCreateDownload_OK(t *testing.T) {
	fake := newFakeQbtAPI()
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fake.props[hash] = qbt.TorrentProperties{PiecesNum: 1, Name: "Movie"}
	fake.files[hash] = qbt.TorrentFiles{{Index: 0, Name: "a.mp4", Size: 100}}

	_, srv := newDownloadTestServer(t, fake)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/download-api/torrents",
		strings.NewReader(`{"magnet":"magnet:?xt=urn:btih:`+hash+`"}`))
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var info DownloadInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Hash != hash || info.Name != "Movie" || len(info.Files) != 1 {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestCreateDownload_BadRequest(t *testing.T) {
	_, srv := newDownloadTestServer(t, newFakeQbtAPI())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/download-api/torrents", strings.NewReader(`{"magnet":""}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateDownload_MetadataTimeout(t *testing.T) {
	fake := newFakeQbtAPI()
	dm, err := newDownloadManagerWithAPI(fake, "/data/downloads", t.TempDir(), "tsa-download", 2*time.Millisecond, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(nil, Config{MetadataTimeout: 20 * time.Millisecond})
	h.SetDownloadManager(dm)
	srv := h.Routes()

	// No props/files ever populated — metadata never arrives.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/download-api/torrents",
		strings.NewReader(`{"magnet":"magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestSelectDownloadFiles_OK(t *testing.T) {
	fake := newFakeQbtAPI()
	_, srv := newDownloadTestServer(t, fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/download-api/torrents/hash1/select", strings.NewReader(`{"indices":[0,2]}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d, body=%s", rec.Code, rec.Body.String())
	}

	calls := fake.getPriorityCalls()
	if len(calls) != 1 || calls[0].hash != "hash1" || calls[0].ids != "0|2" || calls[0].priority != 1 {
		t.Errorf("unexpected priority calls: %v", calls)
	}
}

func TestSelectDownloadFiles_BadRequest(t *testing.T) {
	_, srv := newDownloadTestServer(t, newFakeQbtAPI())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/download-api/torrents/hash1/select", strings.NewReader(`{"indices":[]}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListDownloads_OK(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.torrents["a"] = qbt.Torrent{Hash: "a", Name: "Movie", Category: "tsa-download", Progress: 0.5}
	_, srv := newDownloadTestServer(t, fake)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download-api/torrents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var list []DownloadInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Hash != "a" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestGetDownload_NotFound(t *testing.T) {
	_, srv := newDownloadTestServer(t, newFakeQbtAPI())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download-api/torrents/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetDownload_QbittorrentUnavailable(t *testing.T) {
	fake := newFakeQbtAPI()
	fake.getTorrentsErr = errors.New("qbittorrent unreachable")
	_, srv := newDownloadTestServer(t, fake)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download-api/torrents/hash1", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestDeleteDownload_OK(t *testing.T) {
	fake := newFakeQbtAPI()
	_, srv := newDownloadTestServer(t, fake)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/download-api/torrents/hash1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if len(fake.deleteCalls) != 1 || fake.deleteCalls[0][0] != "hash1" {
		t.Errorf("unexpected delete calls: %v", fake.deleteCalls)
	}
}

func TestStreamDownloadFile_RangeAndFull(t *testing.T) {
	fake := newFakeQbtAPI()
	const hash = "cccc"
	fake.torrents[hash] = qbt.Torrent{Hash: hash}
	fake.props[hash] = qbt.TorrentProperties{SavePath: "/data/downloads/Movie", PieceSize: 1024}
	fake.files[hash] = qbt.TorrentFiles{{Index: 0, Name: "a.mp4", Size: 10}}
	fake.setPieceStates(hash, []qbt.PieceState{qbt.PieceStateAlreadyDownloaded})

	dm, srv := newDownloadTestServer(t, fake)
	// Write the actual bytes where OpenFile will look for them.
	if err := writeTestFile(t, dm, "/data/downloads/Movie", "a.mp4", "0123456789"); err != nil {
		t.Fatal(err)
	}

	url := "/download-api/stream/" + hash + "/0/a.mp4"

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("full: expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if got, _ := io.ReadAll(rec.Body); string(got) != "0123456789" {
		t.Errorf("full body = %q", got)
	}
	if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, "inline") {
		t.Errorf("expected inline disposition by default, got %q", disp)
	}

	// Range request.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Range", "bytes=2-5")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("range: expected 206, got %d", rec.Code)
	}
	if got, _ := io.ReadAll(rec.Body); string(got) != "2345" {
		t.Errorf("range body = %q", got)
	}

	// ?dl=1 triggers an attachment disposition.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url+"?dl=1", nil))
	if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, "attachment") {
		t.Errorf("expected attachment disposition with ?dl=1, got %q", disp)
	}
}

func TestStreamDownloadFile_NotFound(t *testing.T) {
	_, srv := newDownloadTestServer(t, newFakeQbtAPI())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download-api/stream/missing/0/a.mp4", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestStreamDownloadFile_InvalidIndex(t *testing.T) {
	_, srv := newDownloadTestServer(t, newFakeQbtAPI())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download-api/stream/hash1/not-a-number/a.mp4", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// writeTestFile writes content at the local path remotePath maps to under
// dm's downloadDir (mirroring qBittorrent's save_path bind-mount mapping).
func writeTestFile(t *testing.T, dm *DownloadManager, remotePath, name, content string) error {
	t.Helper()
	local, err := mapPath(remotePath, dm.remoteRoot, dm.downloadDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(local, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(local, name), []byte(content), 0o644)
}
