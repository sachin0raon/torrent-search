package stream

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// Handler builds the HTTP router for the streaming service.
type Handler struct {
	mgr *Manager
	cfg Config
}

// NewHandler wires a Handler over a Manager.
func NewHandler(mgr *Manager, cfg Config) *Handler {
	return &Handler{mgr: mgr, cfg: cfg}
}

// Routes returns the service's http.Handler with all endpoints mounted.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /stream-api/sessions", h.createSession)
	mux.HandleFunc("GET /stream-api/sessions/{id}", h.getSession)
	mux.HandleFunc("GET /stream-api/sessions/{id}/stats", h.getSessionStats)
	mux.HandleFunc("DELETE /stream-api/sessions/{id}", h.deleteSession)
	mux.HandleFunc("GET /stream/{id}/{index}/{filename...}", h.streamFile)
	// Some players (e.g. MX Player) strip the filename from the URL and request
	// just /{id}/{index}. Serve directly from this pattern to avoid the 301
	// redirect that Go's mux would otherwise issue to /{id}/{index}/.
	mux.HandleFunc("GET /stream/{id}/{index}", h.streamFile)
	mux.HandleFunc("GET /healthz", h.health)
	return mux
}

type createRequest struct {
	Magnet string `json:"magnet"`
}

type sessionResponse struct {
	SessionID string     `json:"sessionId"`
	Name      string     `json:"name"`
	Ready     bool       `json:"ready"`
	Files     []FileInfo `json:"files"`
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Magnet) == "" {
		writeError(w, http.StatusBadRequest, "missing or invalid magnet")
		return
	}

	ctx, cancel := contextWithTimeout(r, h.cfg.MetadataTimeout)
	defer cancel()

	s, err := h.mgr.AddSession(ctx, strings.TrimSpace(req.Magnet))
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidMagnet):
			writeError(w, http.StatusBadRequest, "invalid magnet")
		case errors.Is(err, ErrAtCapacity):
			writeError(w, http.StatusConflict, "too many active streams, try again shortly")
		case errors.Is(err, ErrMetadataTimeout):
			writeError(w, http.StatusGatewayTimeout, "couldn't fetch torrent info (no peers?)")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusOK, h.sessionResponse(s))
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	s, ok := h.mgr.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusGone, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, h.sessionResponse(s))
}

func (h *Handler) getSessionStats(w http.ResponseWriter, r *http.Request) {
	stats, ok := h.mgr.GetStats(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusGone, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	if h.mgr.Remove(r.PathValue("id")) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "session not found")
}

func (h *Handler) streamFile(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file index")
		return
	}

	reader, info, err := h.mgr.OpenFile(r.PathValue("id"), index)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusGone, "stream expired")
		case errors.Is(err, ErrFileIndex):
			writeError(w, http.StatusNotFound, "file not found")
		default:
			log.Printf("streamer: OpenFile id=%s index=%d: %v", r.PathValue("id"), index, err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	defer reader.Close()
	log.Printf("streamer: streaming id=%s file=%q size=%d", r.PathValue("id"), path.Base(info.Path), info.Size)

	name := path.Base(info.Path)
	// Set the content type explicitly so http.ServeContent does not sniff by
	// reading the first bytes (which could block on not-yet-downloaded pieces).
	w.Header().Set("Content-Type", contentType(name))
	// Provide the filename to players that determine format from Content-Disposition
	// rather than the URL path (e.g. when the player stripped the filename from
	// the URL before requesting).
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	// ServeContent handles Range/If-Range parsing, 206 responses and
	// Accept-Ranges using the reader's Seek to discover the size.
	http.ServeContent(w, r, name, time.Time{}, reader)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) sessionResponse(s *Session) sessionResponse {
	return sessionResponse{
		SessionID: s.ID,
		Name:      s.Name,
		Ready:     h.mgr.Ready(s),
		Files:     h.mgr.Files(s),
	}
}

// videoContentTypes covers extensions that mime.TypeByExtension often misses.
var videoContentTypes = map[string]string{
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".m4v":  "video/x-m4v",
	".ts":   "video/mp2t",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".mp4":  "video/mp4",
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := videoContentTypes[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
