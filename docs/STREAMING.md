# Torrent Streaming Feature — Design Document

> Status: **Implemented** (2026-07-28). Extends the base app described in
> [DESIGN.md](DESIGN.md). Reverses the original "magnet-copy only / no
> torrent-client integration" non-goal for this feature.
>
> **Implementation notes / divergences from the design:**
> - Process supervisor is **supervisord** (the doc's alternative to s6-overlay).
> - On-disk cleanup deletes the torrent's data by its **name** (and infohash) under
>   `/downloads`, since anacrolix lays data out keyed by torrent name, not infohash.
> - Startup wipe clears `/downloads`'s **contents** (not the dir itself, which is a
>   mount point).
> - Player deep-link formats in `frontend/src/playerLinks.js` are the user-supplied
>   set: **VLC** `vlc://{url-without-scheme}`, **MX Player**
>   `intent:{url}#Intent;package=com.mxtech.videoplayer.ad;S.title={fileName};end`,
>   **nPlayer** `nplayer-{url}`. Add players via the single `PLAYERS` table.

---

## 1. Understanding Summary

- **What:** Add a **Stream** action to any magnet-bearing result (Torrentio + Forum
  rows). It turns a torrent into an on-demand HTTP stream and produces **player deep
  links** (`vlc://…`, MX Player, nPlayer, …) that launch an external mobile player and
  start playing, plus a **Copy stream URL** button.
- **Why:** Go straight from "found a torrent" to "watching it on my phone" without
  downloading first.
- **Who:** Personal tool, now reachable from mobile devices over the internet via the VPS.
- **How (high level):** A **new, separate Go microservice** using
  `github.com/anacrolix/torrent` **v1.61.0** runs on the VPS on its own port
  (`127.0.0.1:8001`), behind **nginx** (Approach 1), alongside the unchanged
  Python/FastAPI backend and React SPA. It adds a magnet, fetches metadata, lists files,
  and serves a chosen file over HTTP with range support.
- **File selection:** Always list the torrent's files; the user taps one to get its
  stream link (handles season packs correctly).
- **Lifecycle:** Idle timeout → drop the torrent from memory **and delete its downloaded
  data** from disk.
- **Non-goals (unchanged):** No auth, no accounts, no in-browser transcoding/playback
  (external players do the playing), no persistent library/cache.

---

## 2. Assumptions

1. **Metadata step:** tapping Stream calls the Go service to add the magnet; it fetches
   torrent metadata from DHT/peers/trackers with a timeout (**~45s**); success → session
   id + file list, timeout → error surfaced in the UI.
2. **Streaming:** HTTP endpoint with **range-request** support and sequential/readahead
   piece prioritization for smooth seeking; the raw URL is wrapped into player deep links.
3. **Deep links:** built **client-side** from a small per-player template map (formats
   supplied later); the template table is the only thing that changes to add a player.
4. **Resource caps:** max **~5 concurrent active torrents**; idle timeout **10 min** with
   no stream reads → drop + delete data; container memory/CPU raised for a mid (4 GB+) VPS.
5. **Reachability & base URL:** stream URLs are **same-origin**, built client-side from
   `window.location.origin` (no env var). nginx terminates TLS and proxies to Go, so no
   mixed-content issue.
6. **Security:** endpoints are **fully open** (no auth) — an explicitly accepted risk,
   matching the current tool.
7. **State:** everything ephemeral; a service restart drops all sessions and wipes the
   download dir. Downloaded data lives in a temp dir on a mounted volume (`/downloads`).
8. **Python backend:** unchanged; the Go service owns all torrent logic.

---

## 3. Decision Log

| # | Decision | Alternatives considered | Why chosen |
|---|----------|------------------------|------------|
| 1 | Separate **Go microservice** for streaming (not Python) | Python torrent lib; rewrite backend | `anacrolix/torrent` is the powerful, proven Go lib the user chose; isolates torrent logic |
| 2 | Pin `github.com/anacrolix/torrent` **v1.61.0** | Latest/floating | User-specified exact version for reproducibility |
| 3 | **Approach 1** — nginx front door, single public host, path routing | (2) FastAPI proxies to Go; (3) second public port for Go | Clean same-origin public stream URLs; keeps fast range-serving in Go; Go admin API stays localhost-only |
| 4 | Introduce **nginx + multi-process supervisor** (`s6-overlay`/supervisord) | Keep single-process image | Approach 1 needs one front door running 3 processes in one container |
| 5 | **List files, user picks** (always) | Auto-pick largest; auto-for-movies/list-for-TV | Correct for season packs; simple, predictable |
| 6 | **Idle timeout + delete data** | Keep cache; manual stop only | Lean on a VPS; re-stream re-downloads |
| 7 | Stream on **both Torrentio + Forum** magnet rows | One source only | Consistent, most useful |
| 8 | **Fully open**, no auth | Unguessable tokens; shared secret | Matches personal-tool stance; accepted bandwidth/abuse risk |
| 9 | **~5 concurrent** streams; mid VPS sizing | 1–2 small; unbounded | User expects a few concurrent (family) streams |
| 10 | Public base URL from **`window.location.origin`** | `STREAM_PUBLIC_BASE_URL` env | Same-origin under nginx → browser already knows correct scheme/host; no drift/config |
| 11 | **Copy stream URL** button as a first-class action | Copy as fallback only | User request; universal escape hatch beyond deep links |
| 12 | Augment each torrent with **fetched public trackers** (`AddTrackers`, refreshed) | Magnet trackers + DHT only; static baked-in list | User request to boost speed; live lists stay current; best-effort so failures don't break streaming |

**Accepted risk:** the streaming endpoints are unauthenticated on a public IP — anyone
with a URL can consume bandwidth, and the add-magnet endpoint is an open magnet proxy.
Explicitly accepted for this personal tool.

---

## 4. Final Design

### 4.1 Architecture & container topology

Three processes in one image, fronted by nginx (Approach 1):

```
                         ┌──────────── Docker container ────────────┐
 phone / browser ──443──▶│ nginx (front door)                       │
                         │   /              → static SPA (/app/static)│
                         │   /api/*         → uvicorn  :8000 (FastAPI)│
                         │   /stream-api/*  → go-stream :8001 (admin) │
                         │   /stream/*      → go-stream :8001 (bytes) │
                         │                                            │
                         │  uvicorn :8000  (unchanged Python backend) │
                         │  go-stream :8001 (anacrolix, localhost-only)│
                         └──────────────────────────────────────────┘
                                    volume: /data (config.json)
                                    volume: /downloads (ephemeral torrent data)
```

- **Process management:** move from single `CMD uvicorn …` to **`s6-overlay`** (or
  supervisord) running nginx + uvicorn + go-stream; one container still = one `docker run`.
- **Go service binds `127.0.0.1:8001`** only; nginx is the sole public listener.
- **Build:** add a Go build stage (`golang:1.23`, CGO-off static binary) to the multi-stage
  Dockerfile, copied into runtime; install nginx in runtime.
- **New volume `/downloads`** for in-flight pieces, wiped by idle-GC and on startup;
  separate from the existing `/data` config volume.
- **Resource limits** in compose raised (propose **2 GB memory / 2 CPU**) for ~5 torrents.
- **Repo layout:** new top-level `streamer/` Go module, peer to `backend/` and `frontend/`.

### 4.2 Go streaming service: components

Module `streamer/` (Go 1.23, `anacrolix/torrent v1.61.0`):

- **`Manager`** — owns a single `torrent.Client` (DHT on, sane peer/conn limits) and a
  mutex-guarded `map[sessionID]*Session`. Enforces the **max-5** cap; runs the idle-GC loop.
- **`Session`** — wraps one `*torrent.Torrent`: `sessionID` (random, unguessable),
  magnet/infohash, resolved file list, and `lastRead atomic` touched on every byte served.
- **`handlers`** — thin HTTP layer.
- **`storage`** — anacrolix file storage rooted at `/downloads/{infohash}`.
- The `anacrolix` client sits behind a small interface so tests can use a fake.

### 4.3 HTTP API (all behind nginx)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/stream-api/sessions` | body `{magnet}` → add torrent, await metadata (≤45s), return `{sessionId, name, files:[{index,path,size,streamable}]}`. Reuse existing session if infohash already active. |
| `GET` | `/stream-api/sessions/{id}` | poll/refresh file list & status (slow metadata). |
| `GET` | `/stream/{id}/{fileIndex}/{filename}` | actual byte stream — **range-aware**; sets `Content-Type`/`Content-Length`/`Accept-Ranges`; sequential readahead on the selected file. |
| `DELETE` | `/stream-api/sessions/{id}` | optional explicit stop (drop + delete). |
| `GET` | `/healthz` | liveness for the container healthcheck. |

- **Range serving:** back the file with anacrolix's `Reader` (`SetReadahead` sized for
  streaming) and hand it to Go's `http.ServeContent`, which handles range parsing, `206`,
  and `If-Range` for free.
- **Streamable flag:** extensions in the video allowlist
  (`.mp4 .mkv .avi .mov .m4v .webm .ts`) → `streamable:true`; the UI shows all files but
  only offers Stream on those.

### 4.4 End-to-end data flow

1. User taps **Stream** on a magnet-bearing row.
2. SPA `POST /stream-api/sessions {magnet}`. Manager: infohash already active? → reuse.
   Else capacity < 5? → add; at cap → `409 Too many active streams`.
3. Go awaits `<-t.GotInfo()` with a 45s context. Timeout → drop torrent, `504`.
4. On info: build file list, mark `streamable`, store `Session`, return it.
5. SPA renders a **file picker** (name + size; Stream enabled only on streamable files).
6. User taps a file → SPA composes the absolute URL
   `${window.location.origin}/stream/{id}/{index}/{enc(filename)}` and builds the player
   deep links + Copy-URL button from it.
7. Player opens the URL → `GET /stream/...` → range-served bytes; each read touches
   `lastRead`.

### 4.5 Lifecycle & idle-GC

- **Idle-GC loop** ticks ~30s: `now - lastRead > 10 min` → close torrent,
  `Torrent.Drop()`, and **delete `/downloads/{infohash}`**.
- Also drop on explicit `DELETE`; **wipe `/downloads/*` on startup** (clean slate after
  restart).
- **Concurrency:** `lastRead` atomic; session map mutex-guarded; add-torrent and GC
  coordinate through the Manager so a session can't be GC'd mid-add. Metadata fetch is
  per-request with its own context so one slow magnet never blocks others.
- **Backpressure:** the readahead window bounds how far ahead of the player we buffer,
  capping disk/RAM per stream.

### 4.6 Configuration

| Var | Default | Purpose |
|---|---|---|
| `STREAM_MAX_ACTIVE` | `5` | Active-torrent cap |
| `STREAM_IDLE_TIMEOUT` | `600` (s) | Idle-GC threshold |
| `STREAM_METADATA_TIMEOUT` | `45` (s) | `GotInfo` wait |
| `STREAM_DOWNLOAD_DIR` | `/downloads` | Ephemeral data root |
| `STREAM_TRACKERS_URLS` | 3 public lists | Comma-separated tracker-list URLs; empty/unset = defaults, `none` = disabled |
| `STREAM_TRACKERS_REFRESH` | `21600` (s) | Tracker-list refresh interval (6h) |
| `STREAM_TRACKERS_TIMEOUT` | `15` (s) | Per-list fetch timeout |

No public-base-URL env var — the SPA derives it from `window.location.origin`.

**Tracker augmentation:** on startup (and every `STREAM_TRACKERS_REFRESH`) the Go
service fetches the public tracker lists, parses/dedups them, and applies them to
each new torrent via `Torrent.AddTrackers` (one tracker per BEP-12 tier) to widen
peer discovery and boost speed. Fetching is best-effort — if all lists fail, streaming
still works via the magnet's own trackers + DHT. Default lists:
`cf.trackerslist.com/all.txt`, `ngosang.github.io/trackerslist/trackers_all_http.txt`,
`raw.githubusercontent.com/hezhijie0327/Trackerslist/…/trackerslist_tracker.txt`.

### 4.7 Frontend (React) additions

- `api/streamer.js` — `createSession(magnet)`, `getSession(id)`; talks to `/stream-api/*`.
- `playerLinks.js` — pure builder: stream URL → `{vlc, mx, nplayer, …}` deep links from a
  template table (formats supplied later; adding a player = one table entry).
- `StreamButton.jsx` — on rows with a magnet; opens `StreamModal`.
- `StreamModal.jsx` — spinner during metadata fetch → file list; each streamable file
  offers the player-link buttons, a **Copy stream URL** button, and (for GC'd sessions) a
  Restart action.
- Reads nothing extra from config; stream URL built from `window.location.origin`.

### 4.8 Error handling & edge cases

| Case | Behavior |
|---|---|
| Metadata timeout (no peers) | `504` → modal: "Couldn't fetch torrent info (no peers?)" + Retry |
| At capacity (>5) | `409` → "Too many active streams, try again shortly" |
| Non-video / zero video files | File list shown; Stream disabled on non-streamable; note if none |
| Session GC'd then player reconnects | `410 Gone` → SPA offers "Restart stream" (re-adds magnet) |
| Invalid/duplicate magnet | reuse existing session; malformed → `400` |
| Player app not installed | OS handles the failed deep link; Copy-stream-URL remains |
| App opened via raw `http://IP:port` (dev) | `window.location.origin` reflects it — still correct |

### 4.9 Testing strategy

**Go service (`streamer/`):**
- **Manager:** capacity cap (6th add → `409`), session reuse by infohash, idle-GC drops +
  deletes `/downloads/{infohash}` after timeout (injected fake clock), startup wipe.
- **Handlers** (`httptest`): `POST /stream-api/sessions` shapes (valid → file list,
  malformed → `400`); GC'd session → `410`; range serving via `http.ServeContent` against
  a temp file — assert `206`, `Content-Range`, `Accept-Ranges`, mid-file byte correctness.
- **Metadata timeout:** torrent stub whose `GotInfo` never fires → `504` within deadline.
- **Streamable classifier:** extension allowlist table test.
- No real DHT in CI (fake behind the client interface); one optional
  `//go:build integration` test hits a known public magnet locally, off by default.

**Frontend (Vitest + RTL):**
- `playerLinks.js` builder: correct `vlc://` / MX / nPlayer strings per the template table.
- `api/streamer.js` (fetch mocked): session create, poll, error mapping.
- `StreamModal`: spinner → file list; Stream disabled on non-streamable; `410` → "Restart
  stream"; **Copy stream URL** button; stream URL built from mocked `window.location.origin`.

**Manual smoke checklist:** stream a movie magnet → pick file → open `vlc://` on phone,
seek mid-file; stream a season pack → pick one episode; leave idle >10 min → confirm data
deleted; exceed 5 concurrent → `409`; restart container → `/downloads` clean; use Copy
stream URL in a desktop player.
