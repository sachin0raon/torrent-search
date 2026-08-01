package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// downloadAPITimeout bounds every individual /download-api/* qBittorrent
// call, mirroring qbt_client.go's apiTimeout for the streaming path.
const downloadAPITimeout = 15 * time.Second

// registerDownloadRoutes mounts /download-api/* onto mux. Called from
// Handler.Routes() only when h.dm is non-nil, so the routes are genuinely
// absent — not just erroring — when DOWNLOAD_ENGINE is unset (docs/STREAMING.md
// §6.8).
func registerDownloadRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /download-api/status", h.downloadStatus)
	mux.HandleFunc("POST /download-api/torrents", h.createDownload)
	mux.HandleFunc("POST /download-api/torrents/{hash}/select", h.selectDownloadFiles)
	mux.HandleFunc("GET /download-api/torrents", h.listDownloads)
	mux.HandleFunc("GET /download-api/torrents/{hash}", h.getDownload)
	mux.HandleFunc("DELETE /download-api/torrents/{hash}", h.deleteDownload)
	mux.HandleFunc("GET /download-api/stream/{hash}/{index}/{filename...}", h.streamDownloadFile)
	// Same rationale as the /stream/{id}/{index} fallback above: serve the
	// no-filename form directly rather than 301-redirecting.
	mux.HandleFunc("GET /download-api/stream/{hash}/{index}", h.streamDownloadFile)
}

func (h *Handler) downloadStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
}

func (h *Handler) createDownload(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Magnet) == "" {
		writeError(w, http.StatusBadRequest, "missing or invalid magnet")
		return
	}

	ctx, cancel := contextWithTimeout(r, h.cfg.MetadataTimeout)
	defer cancel()

	info, err := h.dm.AddTorrent(ctx, strings.TrimSpace(req.Magnet))
	if err != nil {
		switch {
		case errors.Is(err, ErrDownloadInvalidMagnet):
			writeError(w, http.StatusBadRequest, "invalid magnet")
		case errors.Is(err, ErrDownloadMetadataTimeout):
			writeError(w, http.StatusGatewayTimeout, "couldn't fetch torrent info (no peers?)")
		default:
			log.Printf("streamer: download add torrent: %v", err)
			writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("qbittorrent unavailable: %v", err))
		}
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type selectFilesRequest struct {
	Indices []int `json:"indices"`
}

func (h *Handler) selectDownloadFiles(w http.ResponseWriter, r *http.Request) {
	var req selectFilesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Indices) == 0 {
		writeError(w, http.StatusBadRequest, "missing or invalid indices")
		return
	}
	ctx, cancel := contextWithTimeout(r, downloadAPITimeout)
	defer cancel()
	if err := h.dm.SelectFiles(ctx, r.PathValue("hash"), req.Indices); err != nil {
		log.Printf("streamer: download select files hash=%s: %v", r.PathValue("hash"), err)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("qbittorrent unavailable: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listDownloads(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, downloadAPITimeout)
	defer cancel()
	list, err := h.dm.List(ctx)
	if err != nil {
		log.Printf("streamer: download list: %v", err)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("qbittorrent unavailable: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) getDownload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, downloadAPITimeout)
	defer cancel()
	info, err := h.dm.Get(ctx, r.PathValue("hash"))
	if err != nil {
		if errors.Is(err, ErrDownloadNotFound) {
			writeError(w, http.StatusNotFound, "torrent not found")
			return
		}
		log.Printf("streamer: download get hash=%s: %v", r.PathValue("hash"), err)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("qbittorrent unavailable: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) deleteDownload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, downloadAPITimeout)
	defer cancel()
	if err := h.dm.Delete(ctx, r.PathValue("hash")); err != nil {
		log.Printf("streamer: download delete hash=%s: %v", r.PathValue("hash"), err)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("qbittorrent unavailable: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) streamDownloadFile(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file index")
		return
	}

	ctx, cancel := contextWithTimeout(r, downloadAPITimeout)
	reader, info, err := h.dm.OpenFile(ctx, r.PathValue("hash"), index)
	cancel()
	if err != nil {
		switch {
		case errors.Is(err, ErrDownloadNotFound):
			writeError(w, http.StatusNotFound, "torrent not found")
		case errors.Is(err, ErrFileIndex):
			writeError(w, http.StatusNotFound, "file not found")
		default:
			log.Printf("streamer: download OpenFile hash=%s index=%d: %v", r.PathValue("hash"), index, err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	defer reader.Close()

	name := path.Base(info.Name)
	// Same rationale as streamFile: set Content-Type explicitly so
	// http.ServeContent doesn't sniff by reading ahead (which could block on
	// a not-yet-downloaded piece).
	w.Header().Set("Content-Type", contentType(name))
	disposition := "inline"
	if r.URL.Query().Get("dl") != "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	http.ServeContent(w, r, name, time.Time{}, reader)
}
