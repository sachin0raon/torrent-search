# Torrent Search Aggregator

A locally-run web app to search torrents for movies/TV by title, aggregating two
sources with one-click magnet copy. Guided flow: **TMDB → IMDb → torrentio + forum**.

See [docs/DESIGN.md](docs/DESIGN.md) for the full design, assumptions, and decision log.

- **Backend:** FastAPI (Python 3.11+), async httpx, BeautifulSoup4 — proxies TMDB,
  torrentio, and a configurable forum site; parses streams/HTML; builds magnets.
- **Frontend:** React (Vite) SPA — thin renderer, tabbed results (Torrentio / Forum).
- Local single-user, no auth.

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
# Backend (39 tests)
cd backend && .venv/bin/python -m pytest -q

# Frontend (9 tests)
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

## Project layout

```
backend/    FastAPI app, services (tmdb/torrentio/forum), tests + fixtures
frontend/   Vite React SPA (components, api client, tests)
docs/       DESIGN.md
```
