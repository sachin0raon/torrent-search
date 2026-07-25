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
| Forum base URL override | UI ⚙️ Settings | Persisted to `backend/config.json`; overrides the `.env` default and survives restarts. |
| Torrentio source (client/server) | UI ⚙️ Settings | Persisted per-browser in `localStorage` (key `torrentioMode`, default **client**). Client fetches Torrentio from the browser to avoid server-side Cloudflare `403`s; server uses the backend. |
| `DISCOVER_CACHE_TTL_SECONDS` | `backend/.env` | TTL (seconds) for the in-process cache of Discover rail responses (trending/popular/top-rated). Default `3600`. Lower = fresher lists, more TMDB calls; higher = fewer calls, staler lists. |
| Active Discover badge | UI (below search bar) | Persisted per-browser in `localStorage` (key `discoverActiveBadge`, default none active). |

## Run with Docker

A single multi-stage image builds the React SPA and serves it (same origin) from
the FastAPI backend, so everything runs on one port with no CORS setup.

### docker compose (recommended)

```bash
export TMDB_API_KEY=your_tmdb_api_key
export FORUM_BASE_URL=https://your-forum.tld   # optional; also settable in the UI
docker compose up --build
```

Open http://localhost:8000.

### plain docker

```bash
docker build -t torrent-search-aggregator:latest .
docker run --rm -p 8000:8000 \
  -e TMDB_API_KEY=your_tmdb_api_key \
  -e FORUM_BASE_URL=https://your-forum.tld \
  -v tsa_config:/data \
  torrent-search-aggregator:latest
```

Notes:
- Runs as a non-root user; image is ~270 MB.
- The forum-base-URL override (set from the UI) persists in the `/data` volume
  (`CONFIG_JSON_PATH=/data/config.json`).
- `TMDB_API_KEY` is passed at runtime and never baked into the image.
- Health check: `GET /api/health`.

## Tests

```bash
# Backend (70 tests)
cd backend && .venv/bin/python -m pytest -q

# Frontend (35 tests)
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

## Project layout

```
backend/    FastAPI app, services (tmdb/torrentio/forum/discover_cache), tests + fixtures
frontend/   Vite React SPA (components incl. Discover badges, api client, tests)
docs/       DESIGN.md
```
