# Torrent Search Aggregator

A locally-run web app to search torrents for movies/TV by title, aggregating two
sources with one-click magnet copy. Guided flow: **TMDB → IMDb → torrentio + forum**.

See [docs/DESIGN.md](docs/DESIGN.md) for the full design, assumptions, and decision log.

- **Backend:** FastAPI (Python 3.11+), async httpx, BeautifulSoup4 — proxies TMDB,
  torrentio, and a configurable forum site; parses streams/HTML; builds magnets.
- **Frontend:** React (Vite) SPA — thin renderer, tabbed results (Torrentio / Forum),
  with a per-browser toggle to fetch Torrentio client-side or server-side.
- **Discover:** 6 badges below the search bar — Trending / Popular / All-Time
  Favorites, movies and TV — for finding something without typing a query. Click a
  badge to show its list (one visible at a time); click it again to hide it.
  Auto-hides once any title is picked (search or Discover). Backed by a cached
  `/api/discover` endpoint.
- **Stream:** any magnet-bearing row (Torrentio or Forum) has a **Stream** button.
  Tap it to fetch torrent metadata, pick a file, and launch an external player via
  deep link — `vlc://`, MX Player Android intent, or `nplayer-` — or copy the raw
  stream URL. Powered by a Go microservice (`anacrolix/torrent v1.61.0`) running
  alongside the backend. Idle streams are dropped after 10 minutes and their
  downloaded data deleted. See [docs/STREAMING.md](docs/STREAMING.md).
- **Scroll-to-top:** a floating button, bottom-right, appears once you've scrolled
  down and smooth-scrolls back to top.
- Local single-user, no auth.

> **Torrentio source (client vs server).** Torrentio sits behind Cloudflare, which
> can reject a server's datacenter IP with a `403` (common on VPS deployments). The
> UI ⚙️ Settings has a toggle — **client** mode fetches Torrentio directly from the
> browser (your residential IP, bypassing that block), while **server** mode uses
> the backend. When a Torrentio call fails, a **Retry** button re-runs it in the
> currently selected mode. Both paths use the same retry policy (3 attempts,
> exponential backoff + jitter, retrying 429/5xx/network but not `403`).

## Prerequisites

- Python 3.11+ (the repo uses [uv](https://github.com/astral-sh/uv))
- Node.js 18+
- Go 1.24+ (only needed to run the streamer outside Docker)

## Setup

### Backend

```bash
cd backend
cp .env.example .env          # then edit: set TMDB_API_KEY and FORUM_BASE_URL
uv venv --python 3.11
uv pip install -e ".[dev]"
```

Run the API (http://localhost:8000):

```bash
cd backend
.venv/bin/uvicorn app.main:app --reload --port 8000
```

### Frontend

```bash
cd frontend
npm install
npm run dev                    # http://localhost:5173 (proxies /api to :8000)
```

Open http://localhost:5173.

## Configuration

| Setting | Where | Notes |
|---------|-------|-------|
| `TMDB_API_KEY` | `backend/.env` | Server-side only; never sent to the browser. |
| `FORUM_BASE_URL` | `backend/.env` | Default forum base URL. |
| `ENABLE_STREAMING` | `backend/.env` | Toggle streaming UI capability (`true`/`false`, default `true`). Set to `false` to hide Stream buttons across the app. |
| `FILE_BROWSER_URL` | `backend/.env` | Optional URL for an external file browser button in the UI. |
| Forum base URL override | UI ⚙️ Settings | Persisted to `backend/config.json`; overrides the `.env` default and survives restarts. |
| Torrentio source (client/server) | UI ⚙️ Settings | Persisted per-browser in `localStorage` (key `torrentioMode`, default **client**). Client fetches Torrentio from the browser to avoid server-side Cloudflare `403`s; server uses the backend. |
| `DISCOVER_CACHE_TTL_SECONDS` | `backend/.env` | TTL (seconds) for the in-process cache of Discover rail responses (trending/popular/top-rated). Default `3600`. Lower = fresher lists, more TMDB calls; higher = fewer calls, staler lists. |
| Active Discover badge | UI (below search bar) | Persisted per-browser in `localStorage` (key `discoverActiveBadge`, default none active). |
| `STREAM_MAX_ACTIVE` | `docker-compose.yml` / env | Max concurrent active torrents (default `5`). |
| `STREAM_IDLE_TIMEOUT` | `docker-compose.yml` / env | Seconds of idle (no stream reads) before a torrent is dropped and its data deleted (default `600`). |
| `STREAM_METADATA_TIMEOUT` | `docker-compose.yml` / env | Seconds to wait for torrent metadata from DHT/peers before returning a timeout error (default `45`). |
| `STREAM_HALF_OPEN_CONNS_PER_TORRENT` | env | Max outbound half-open connections per torrent for anacrolix engine (default `100`). |
| `STREAM_TOTAL_HALF_OPEN_CONNS` | env | Global cap on simultaneous outbound half-open connections (default `500`). |
| `STREAM_ESTABLISHED_CONNS_PER_TORRENT` | env | Max established connections per torrent (default `200`). |
| `STREAM_TRACKERS_URLS` | `docker-compose.yml` / env | Comma-separated URLs of public tracker lists to fetch and add to every torrent. Empty/unset = use built-in defaults; `none` = disable tracker augmentation. |
| `STREAM_TRACKERS_REFRESH` | `docker-compose.yml` / env | How often (seconds) to refresh the tracker lists (default `21600` = 6 h). |
| `STREAM_ENGINE` | env | BitTorrent **streaming** engine: `anacrolix` (default, unchanged behavior) or `qbittorrent`. See [docs/STREAMING.md §5](docs/STREAMING.md) for the qBittorrent-engine design. Cannot be `qbittorrent` at the same time as `DOWNLOAD_ENGINE=qbittorrent` — the streamer fails to start if both are set (see §6). |
| `DOWNLOAD_ENGINE` | env | Persistent **download-manager** feature: unset (default, feature absent) or `qbittorrent`. Independent of `STREAM_ENGINE` — adds a separate "Download" action/page. See [docs/STREAMING.md §6](docs/STREAMING.md). |
| `STREAM_QBIT_HOST` | env | Base URL of a running qBittorrent Web UI (e.g. `http://localhost:8080`). Empty disables qBittorrent entirely on the default (`anacrolix`) engine; **required** when `STREAM_ENGINE=qbittorrent` or `DOWNLOAD_ENGINE=qbittorrent`. |
| `STREAM_QBIT_USER` / `STREAM_QBIT_PASS` | env | qBittorrent Web UI credentials (default `admin` / `adminadmin`); shared by both the streaming and download engines. |
| `STREAM_QBIT_REMOTE_ROOT` | env | Save-path root as qBittorrent itself sees it (e.g. `/data/downloads`). **Required** when `STREAM_ENGINE=qbittorrent` or `DOWNLOAD_ENGINE=qbittorrent`. |
| `STREAM_QBIT_DOWNLOAD_DIR` | env | Local filesystem root the streamer container sees for that same directory (the bind-mount target). **Required** when `STREAM_ENGINE=qbittorrent` or `DOWNLOAD_ENGINE=qbittorrent`; validated at startup. |
| `STREAM_QBIT_CATEGORY` | env | qBittorrent category tag applied to torrents the **streaming** qbittorrent engine adds (default `tsa-stream-engine`); purged on every startup for a clean slate. |
| `STREAM_QBIT_POLL_INTERVAL` | env | Seconds between metadata-readiness and piece-state polls; shared by both the streaming and download engines (default `1`). |
| `STREAM_QBIT_PAUSE_TIMEOUT` | env | Seconds a **streaming** qbittorrent-engine session may go idle before it's paused (download+upload stopped) rather than removed (default `60`). Resumable via a fresh Stream click, a reconnect, or the Active Streams panel. See [docs/STREAMING.md §7](docs/STREAMING.md). |
| `STREAM_QBIT_RETENTION_TIMEOUT` | env | Seconds a **paused** streaming session may stay paused, measured from when it was paused, before it's actually removed (default `86400` = 1 day). Applies the same to complete and incomplete downloads. See [docs/STREAMING.md §7](docs/STREAMING.md). |
| `DOWNLOAD_QBIT_CATEGORY` | env | qBittorrent category tag applied to torrents the **download-manager** feature adds (default `tsa-download`). **Never** purged on startup — downloads are intentionally persistent. See [docs/STREAMING.md §6](docs/STREAMING.md). |
| `DOWNLOAD_UNSELECTED_TIMEOUT` | env | Seconds a download-manager torrent may sit with **no file ever selected** (e.g. opened the file picker, never picked anything) before it's automatically removed from qBittorrent (default `900` = 15 min). A torrent with at least one selected file is never touched by this, regardless of age. See [docs/STREAMING.md §6](docs/STREAMING.md). |

## Run with Docker

A single multi-stage image builds the React SPA, the Go streamer binary, and the
Python backend, then serves everything from one nginx front door on port 8080
inside the container (mapped to host port 8000). No CORS setup needed.

### docker compose (recommended)

```bash
export TMDB_API_KEY=your_tmdb_api_key
export FORUM_BASE_URL=https://your-forum.tld      # optional; also settable in the UI
# export STREAM_TRACKERS_URLS=none                # disable tracker augmentation
docker compose up --build
```

Open http://localhost:8000.

### plain docker

Command containing all configurable environment variables:

```bash
docker build -t torrent-search-aggregator:latest .
docker run --rm -p 8000:8080 \
  -e TMDB_API_KEY=your_tmdb_api_key \
  -e FORUM_BASE_URL=https://your-forum.tld \
  -e ENABLE_STREAMING=true \
  -e FILE_BROWSER_URL=https://filebrowser.yourdomain.com \
  -e DISCOVER_CACHE_TTL_SECONDS=3600 \
  -e FORUM_PROBE_ENABLED=true \
  -e FORUM_PROBE_INTERVAL_MINUTES=30 \
  -e FORUM_PROBE_QUERY=a \
  -e STREAM_ENGINE=anacrolix \
  -e STREAM_MAX_ACTIVE=5 \
  -e STREAM_IDLE_TIMEOUT=600 \
  -e STREAM_METADATA_TIMEOUT=45 \
  -e STREAM_TORRENT_PORT=6881 \
  -e STREAM_HALF_OPEN_CONNS_PER_TORRENT=100 \
  -e STREAM_TOTAL_HALF_OPEN_CONNS=500 \
  -e STREAM_ESTABLISHED_CONNS_PER_TORRENT=200 \
  -e STREAM_TRACKERS_URLS= \
  -e STREAM_TRACKERS_REFRESH=21600 \
  -e DOWNLOAD_ENGINE=qbittorrent \
  -e STREAM_QBIT_HOST=http://localhost:8080 \
  -e STREAM_QBIT_USER=admin \
  -e STREAM_QBIT_PASS=adminadmin \
  -e STREAM_QBIT_REMOTE_ROOT=/data/downloads \
  -e STREAM_QBIT_DOWNLOAD_DIR=/downloads \
  -e STREAM_QBIT_CATEGORY=tsa-stream-engine \
  -e STREAM_QBIT_POLL_INTERVAL=1 \
  -e STREAM_QBIT_PAUSE_TIMEOUT=60 \
  -e STREAM_QBIT_RETENTION_TIMEOUT=86400 \
  -e DOWNLOAD_QBIT_CATEGORY=tsa-download \
  -e DOWNLOAD_UNSELECTED_TIMEOUT=900 \
  -v tsa_config:/data \
  -v tsa_downloads:/downloads \
  torrent-search-aggregator:latest
```

Notes:
- Runs as a non-root user (UID 1001); image is ~400 MB (includes Go streamer binary).
- Container port is **8080** (nginx); map to whichever host port you prefer (`-p 8000:8080`).
- The forum-base-URL override (set from the UI) persists in the `/data` volume
  (`CONFIG_JSON_PATH=/data/config.json`).
- Torrent download data lives in `/downloads` (ephemeral; wiped on restart and by
  idle-GC after 10 min of inactivity). Mount it as a named volume so pieces survive
  container updates.
- `TMDB_API_KEY` is passed at runtime and never baked into the image.
- Health check: `GET /api/health` (uvicorn) or `GET /healthz` (Go streamer).

## Tests

```bash
# Backend (70 tests)
cd backend && .venv/bin/python -m pytest -q

# Go streamer (race-clean)
cd streamer && go test -race ./...

# Frontend (47 tests)
cd frontend && npm test
```

## Manual smoke checklist

1. **Movie flow:** search a movie → click a title → Torrentio tab lists results →
   **Copy magnet** shows "Copied ✓".
2. **TV whole-series:** search a show → click it → leave season/episode empty →
   **Find torrents** → results appear.
3. **TV episode:** same, but set season + episode → episode-specific results.
4. **Forum expand:** Forum tab → **Show links** on a row → file/magnet links load
   inline (lazy fetch); copy a forum magnet.
5. **Partial failure:** set a bad forum base URL in ⚙️ Settings → run a search →
   Torrentio tab still works; Forum tab shows an inline error banner.
6. **No IMDb ID:** pick an obscure title with no IMDb mapping → notice shown,
   torrentio skipped, forum still runs.
7. **Config persistence:** set a forum base URL in Settings → restart backend →
   value is retained (read from `config.json`).
8. **Torrentio source toggle:** in ⚙️ Settings switch between client/server →
   re-run a search → Torrentio results still appear; the choice persists across
   reloads (browser `localStorage`).
9. **Retry:** when the Torrentio tab shows an error, click **Retry** → it re-runs
   in the current mode (works with a mode switch in between, e.g. server 403 →
   flip to client → Retry succeeds).
10. **Discover:** click a badge (e.g. **Trending Movies**) below the search bar →
    its list loads; click a different badge → the first hides, the second loads;
    click the active badge again → it hides. **Load more** appends another page;
    click a poster → same flow as a search result (season/episode picker for TV,
    then streams), and the Discover panel auto-hides once you do. Click
    **← Change title** → the same badge reappears with its list intact (no
    refetch). Switch away from a badge and back → instant, no refetch. Reload
    the page → the badge you last explicitly clicked is still restored, even
    if it auto-hid earlier.
11. **Discover query isolation:** type a search (e.g. "batman") → get results →
    instead of clicking a result, click a Discover badge and pick a title from
    it → the forum search uses that title's own name, not "batman".
12. **Scroll-to-top:** scroll down past a screen or so → a floating button
    appears bottom-right → click it → page smooth-scrolls back to top, then the
    button disappears again.
13. **Stream file list:** click **Stream** on any Torrentio or Forum magnet row →
    a modal spins up, fetches metadata, and lists the torrent's files (name + size;
    non-video files shown but Stream button disabled on them).
14. **Player deep links:** click a file's **VLC / MX Player / nPlayer** button →
    the device launches the matching player and starts playing; seek mid-file to
    confirm range requests.
15. **Copy stream URL:** click **Copy stream URL** next to any streamable file →
    paste into a desktop player (e.g. VLC's "Open Network") and confirm playback.
16. **Idle GC:** start a stream, leave it idle > 10 min → the session is dropped
    and `/downloads` data is deleted; the player gets a `410 Gone` and the modal
    offers **Restart stream**.
17. **Capacity:** add 5 streams, try a 6th → modal shows "Too many active streams,
    try again shortly" (`409`).
18. **Tracker augmentation:** check the Go service logs on startup — it should log
    "loaded N trackers from M sources"; set `STREAM_TRACKERS_URLS=none` to disable.

## Project layout

```
backend/    FastAPI app, services (tmdb/torrentio/forum/discover_cache), tests + fixtures
frontend/   Vite React SPA (components incl. Discover, Stream, api client, tests)
streamer/   Go microservice — torrent session manager, HTTP stream server, tracker augmentation
deploy/     nginx.conf, supervisord.conf (used by the Docker image)
docs/       DESIGN.md, STREAMING.md
```
