# Torrent Search Aggregator — Design Document

> Status: **Design approved** (brainstorming complete). Ready for implementation handoff.
> Date: 2026-07-23
>
> **Update (2026-07-23):** added a per-browser **client/server Torrentio fetch toggle**
> (client mode calls torrentio.strem.fun directly from the browser to bypass
> datacenter-IP `403`s behind Cloudflare) and a **Retry** action on Torrentio
> failures. See Decision Log #15 and §4.2a.

---

## 1. Understanding Summary

- **What:** A locally-run web app (React/Vite SPA + FastAPI backend) to search torrents for movies/TV by title, aggregating two sources, with one-click magnet copy.
- **Why:** Personal tool to quickly find torrents via a guided TMDB → IMDb → torrent-provider flow without manually juggling APIs and a scraping forum.
- **Who:** Single user, running locally, no auth.
- **Core flow:** (1) TMDB `search/multi` → pick title → (2) fetch `external_ids` for `imdb_id` → (3) **parallel** fan-out to **torrentio** (magnets ready) + **configurable forum site** (title + `tid` only) → (4) tabbed results; forum rows expand on click to fetch & parse their topic HTML for torrent/magnet links.
- **Key constraints:** Backend required (CORS + HTML scraping). Torrentio params hardcoded (`sort=seeders|quality`, `qualityfilter=480p`). Forum base URL: `.env` default, UI-overridable, persisted to backend `config.json`. TV supports both season/episode selection and whole-series calls.
- **Non-goals:** No download/torrent-client integration (magnet copy only), no auth, no multi-user, no accounts, no persistent search history/DB.

---

## 2. Assumptions

1. **Magnet building (torrentio):** `magnet:?dn={enc(line1_title)}&xt=urn:btih:{infoHash}` + one `&tr=` per `sources[]` entry that starts with `tracker:`. **Full `tracker:` prefix is retained** inside the encoded `&tr=` value. `dn` = first line of the newline-separated `title`.
2. **Parsed torrentio fields:** from the newline-separated `title` — display title (line 1), seeders (after 👤), size (after 💾), provider (after ⚙️). Each metadata field optional.
3. **TV UI:** picking a TV title shows optional season+episode inputs; empty → whole-series call (`/series/{imdb_id}.json`), filled → `/series/{imdb_id}:{s}:{e}.json`. Forum search uses raw typed title text regardless of movie/TV.
4. **Forum topic URL slug:** built from the forum result's `title` field. lowercase → remove special chars except hyphen → spaces to hyphens → collapse repeats → truncate to ≤80 chars. **Trailing hyphen allowed.**
5. **Forum HTML parse:** pair each `ipsAttachLink` / `ipsAttachLink_block` anchor (file link + filename text) with the **next** `href^="magnet:"` anchor in document order.
6. **Results display:** **tabbed** — separate Torrentio and Forum tabs. No cross-source dedup.
7. **Backend** is a stateless proxy except for the single persisted `config.json` (forum base URL). No caching, no database.
8. **Non-functional defaults:** single-user local scale (no rate limiting/hardening); ~10s timeout on all outbound calls; TMDB key only in `.env`, never sent to the browser.
9. **Forum search query (`q=`)** = the raw text the user typed (not the TMDB-resolved title).

---

## 3. Decision Log

| # | Decision | Alternatives considered | Why chosen |
|---|----------|------------------------|------------|
| 1 | Python (FastAPI) backend + React (Vite) SPA | Full JS (Node+React); Python + simple HTML frontend | User chose; fits PycharmProjects env; good async HTTP + scraping libs |
| 2 | Local single-user, no auth | Hosted/multi-user | Personal tool; avoids auth/rate-limit/abuse complexity (YAGNI) |
| 3 | Backend-orchestrated (Approach 1) | Thin proxy + frontend orchestration; hybrid w/ direct TMDB from browser | Keeps TMDB key server-side; parsing in testable Python; thin frontend; solves CORS |
| 4 | TV: season/episode selection **and** whole-series call | Movies-only for torrentio; treat TV identical to movie | User wants both; empty episode → season-pack/complete, filled → specific episode |
| 5 | Torrentio quality/sort **hardcoded** (`sort=seeders\|quality`, `qualityfilter=480p`) | User-selectable in UI; configurable default | User chose literal spec compliance |
| 6 | Forum base URL: `.env` default → UI override → persisted to `config.json` | Env-only; localStorage-only | Survives restarts; editable when the site URL changes |
| 7 | Results display: **tabbed** | Single merged list; two sections | Easier to browse; sources behave differently (magnet-ready vs lazy) |
| 8 | Failure handling: partial results + inline per-source error banner | Fail whole search; silent partial | Best UX for flaky services; keeps usable data visible |
| 9 | Magnet `&tr=` retains full `tracker:` prefix | Strip `tracker:` prefix | User correction — prefix must be part of `&tr=` |
| 10 | Slug allows trailing hyphen | Strip trailing hyphen | User correction |
| 11 | Forum `q=` uses raw typed text | TMDB-resolved title | User confirmed (open question B) |
| 12 | No E2E (Playwright) for v1 | Full E2E suite | YAGNI for local tool; unit tests cover parsing risk |
| 13 | UI visual style: dark theme (blue accent, card rows) | Light theme; light/dark toggle | User confirmed; sensible default for a media tool |
| 14 | UI layout: single-page vertical wizard | Two-pane list+detail; results modal | User confirmed; simplest linear flow matching the staged data |
| 15 | Torrentio fetch: per-browser **client/server toggle** (default client), persisted in `localStorage` | Server-only (original); always client | VPS/datacenter IPs get Cloudflare `403`; client mode uses the browser's residential IP. Backend left untouched — client mode still calls `/api/streams` for Forum and discards the server's Torrentio half. Client fetch mirrors the server retry policy (3 attempts, exp backoff + jitter). |
| 16 | **Retry** button on Torrentio failure; re-runs in the currently selected mode | Auto-retry only; full re-search | Covers transient blips and lets a user flip server→client then retry without re-searching |

---

## 4. Final Design

### 4.1 Architecture

Two processes over HTTP on localhost:

```
React (Vite) SPA :5173  ⇄  FastAPI backend :8000
        │                   outbound (httpx async):
        │                    • TMDB API
        │                    • torrentio.strem.fun
        │                    • forum base URL (configurable)
        └─────────────────► torrentio.strem.fun     (client mode only; browser's
                                                      residential IP, CORS: allow-*)
```

**Backend layout (`backend/`):**
```
app/
  main.py            # FastAPI app, CORS (allow localhost:5173), router mount
  config.py          # loads .env + reads/writes local config.json (forum base URL)
  routers/
    search.py        # /api/search, /api/external-ids
    streams.py       # /api/streams  (parallel torrentio + forum search)
    forum.py         # /api/forum/topic (fetch+parse one topic page)
    settings.py      # GET/PUT /api/config
  services/
    tmdb.py          # search_multi(), external_ids()
    torrentio.py     # fetch_streams(), parse + build_magnet()
    forum.py         # search(), build_topic_url(), fetch_topic() + HTML parse
  models.py          # Pydantic response schemas
  utils/magnet.py    # magnet builder, url-encoding helpers
tests/               # pytest — parsing/magnet/slug logic + fixtures
.env                 # TMDB_API_KEY, FORUM_BASE_URL default
config.json          # persisted forum base URL override (gitignored)
```

**Frontend layout (`frontend/`):** Vite React app — `api/client.js` (backend fetch
wrapper), `api/torrentio.js` (client-side Torrentio fetch/parse/magnet, mirrors the
backend service), `torrentioMode.js` (client/server toggle persisted in
`localStorage`), `components/` (SearchBar, TitleList, SeasonEpisodePicker, ResultTabs,
TorrentioTab, ForumTab, ForumTopicRow, CopyButton, SettingsModal), `App.jsx`
orchestrating the wizard.

**Config precedence:** `config.json` value (if present & non-empty) → else `FORUM_BASE_URL` from `.env`. Re-read per request (cheap, always fresh).

**Stack:** Python 3.11+, FastAPI, httpx (async), BeautifulSoup4 (`lxml`), pydantic v2, pytest. Frontend: React 18, Vite, plain fetch.

### 4.1a UI Style & Layout

- **Visual style:** dark theme — dark background, blue accent, card-based rows,
  system font. Theme tokens live as CSS variables in `frontend/src/styles.css`.
- **Layout:** single-page vertical wizard. Sections reveal progressively as the user
  advances: search bar → title cards (with posters) → season/episode inputs (TV only)
  → tabbed results (Torrentio / Forum) → result rows with copy-magnet.
- **Feedback:** inline spinners per stage, dismissible per-source error banners,
  a **Retry** button on Torrentio failures, transient "Copied ✓" on magnet copy,
  and a notice when a title has no IMDb ID.
- **Responsive/touch:** ≥44px touch targets on coarse-pointer devices, and result/
  forum-link rows reflow (wrap) under 768px so action buttons aren't crushed.

### 4.2 Data Flow & Endpoints

**Stage 1 — Search titles:** `GET /api/search?query={q}` → TMDB `search/multi` → `[{tmdb_id, media_type, title, year, poster_url, overview}]`. Keep only `movie`/`tv`. Frontend stashes raw `q` for forum reuse.

**Stage 2 — Resolve imdb_id:** `GET /api/external-ids?media_type={movie|tv}&tmdb_id={id}` → movie/tv `external_ids` → `{imdb_id}`. Null → skip torrentio, show notice, forum still runs.

**Stage 2.5 — TV only:** optional season/episode inputs before fetching streams.

**Stage 3 — Parallel fan-out:** `GET /api/streams?imdb_id={id}&media_type={m}&raw_query={q}[&season=&episode=]`.
Runs both concurrently via `asyncio.gather(..., return_exceptions=True)`:
- **torrentio:** URL `/movie/{imdb_id}.json`, or `/series/{imdb_id}.json`, or `/series/{imdb_id}:{s}:{e}.json`; base `https://torrentio.strem.fun/sort=seeders|qualityfilter=480p/stream`; parse streams, build magnets.
- **forum search:** `GET {base}/search/api/search.php?q={raw_query}&priority=1&sort=title_asc&page=1&per_page=25`; for each result build topic URL.

Response:
```json
{
  "torrentio": { "ok": true,  "items": [ {"title","seeders","size","source","magnet"} ] },
  "forum":     { "ok": false, "error": "...", "items": [ {"tid","title","topic_url"} ] }
}
```

**Stage 4 — Forum topic expand (lazy):** `GET /api/forum/topic?url={topic_url}` → fetch HTML, parse paired file/magnet links → `[{filename, file_url, magnet}]`.

**Timeouts:** every outbound call ~10s; timeout → that source's `error`.

### 4.2a Torrentio source mode (client vs server) & retry

A per-browser toggle in ⚙️ Settings (persisted in `localStorage`, key `torrentioMode`,
default **client**) selects where Torrentio is fetched:

- **server** — original flow: `/api/streams` returns both Torrentio and Forum.
- **client** — the browser fetches Torrentio directly from `torrentio.strem.fun`
  (works because it sends `access-control-allow-origin: *`) using the visitor's
  residential IP, sidestepping the Cloudflare `403` that a datacenter/VPS IP hits.
  The frontend still calls `/api/streams` for the **Forum** half and **discards the
  server's Torrentio result**, so the backend is unchanged. The two halves are merged
  client-side into the same `{ torrentio, forum }` shape.

`api/torrentio.js` mirrors `services/torrentio.py`: same URL building, title parsing,
and `quote_plus`-identical magnet construction, so client- and server-built magnets
match byte-for-byte. Its retry policy mirrors the backend — **3 attempts, exponential
backoff + jitter** (base 0.3s, cap 3s), overall 15s budget, retrying `429`/`5xx`/network
but **not** `403`.

**Retry:** when the Torrentio tab shows an error (either mode), a **Retry** button
re-runs the fetch. The mode is read at click time, so a user can flip server→client
in Settings and retry without re-searching.

### 4.3 Parsing & Magnet Logic

**Torrentio title parsing:** `title.split("\n")[0]` = display title. Metadata via regex on emoji anchors: seeders after 👤, size after 💾, provider after ⚙️ (each optional → `None`/`—`).

**Magnet builder:**
```
magnet:?dn={quote_plus(line1_title)}&xt=urn:btih:{infoHash}
   for s in sources if s.startswith("tracker:"):
       &tr={quote_plus(s)}      # full string incl. "tracker:" prefix
```
No `sources` → magnet has `dn` + `xt` only.

**Forum slug:**
```python
slug = title.lower()
slug = re.sub(r"[^a-z0-9\s-]", "", slug)   # keep alnum, space, hyphen
slug = re.sub(r"\s+", "-", slug.strip())    # spaces → hyphens
slug = re.sub(r"-+", "-", slug)             # collapse repeats
slug = slug[:80]                            # ≤80 chars; trailing hyphen allowed
```
Topic URL: `{base}/index.php?/topic/{tid}-{slug}/`

**Forum topic HTML parse:** walk anchors in document order.
- file anchors: `class` contains `ipsAttachLink`/`ipsAttachLink_block` → `{filename: text, file_url: href}`.
- magnet anchors: `href` starts with `magnet:`.
- pairing: each file anchor ↔ the next magnet anchor after it. Unpaired file → `magnet: null`; stray magnet → own row with `filename: null`.

### 4.4 Error Handling & Edge Cases

| Case | Behavior |
|---|---|
| TMDB returns `person`/other | Filtered out server-side |
| `imdb_id` missing | Skip torrentio, run forum, show notice |
| Torrentio `streams: []` | Empty tab: "No torrentio results" |
| Forum `results: []` | Empty tab: "No forum results" |
| Forum base URL unset/blank | 400 from `/api/streams`; prompt to set in Settings |
| Topic page has 0 links | "No links found on topic page" |
| Outbound timeout (~10s) | Source → `error: "timed out"` |
| Non-2xx from provider | Source's `error` with status code |
| Malformed torrentio title | Fallbacks (title-only, seeders `—`) |

**Config:** `GET /api/config` → `{forum_base_url, source: "config"|"env"}`. `PUT /api/config {forum_base_url}` → validate http(s) URL, strip trailing slash, write `config.json`.

**Security (local scope):** TMDB key server-side only, never in responses. CORS restricted to `http://localhost:5173`. No auth by design.

### 4.5 Testing Strategy

**Backend (pytest, priority = parsing):**
- `utils/magnet.py`: multiple `tracker:` sources retained/encoded; mixed sources filtered; no `sources`; special chars in title.
- `services/torrentio.py`: normal / missing emoji / empty / malformed title fixtures.
- `services/forum.py`: slug (special chars, unicode, >80 truncation, collapse, trailing hyphen); topic HTML parser (pairing, unpaired file, stray magnet, zero links).
- `config.py`: env vs config.json precedence; trailing-slash normalization; invalid URL rejected.
- Routers: FastAPI `TestClient` + mocked httpx (`respx`/monkeypatch); `/api/streams` partial result on one task raising; missing `imdb_id` skips torrentio.

**Frontend (Vitest + RTL, lighter):** API client (fetch mocked); tab states (results/empty/error); copy-magnet clipboard (mocked); wizard transitions. No Playwright E2E in v1.

**Fixtures:** real samples in `tests/fixtures/` (torrentio JSON, forum search JSON, saved topic HTML) for offline deterministic tests.

**Manual smoke checklist:** search → pick movie → copy magnet; pick TV with/without episode; expand forum topic; break base URL → see error banner; toggle Torrentio client/server in Settings (persists across reloads); on a Torrentio failure use **Retry** (including server→client flip then retry).
