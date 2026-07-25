# Torrent Search Aggregator — Design Document

> Status: **Design approved** (brainstorming complete). Ready for implementation handoff.
> Date: 2026-07-23
>
> **Update (2026-07-23):** added a per-browser **client/server Torrentio fetch toggle**
> (client mode calls torrentio.strem.fun directly from the browser to bypass
> datacenter-IP `403`s behind Cloudflare) and a **Retry** action on Torrentio
> failures. See Decision Log #15 and §4.2a.
>
> **Update (2026-07-25):** added a **Discover** section (Trending/Popular/Top Rated
> rails, movie+TV) below the search bar, so users can browse without a search query.
> See Decision Log #17-24 and §4.6.
>
> **Update (2026-07-25b):** reworked Discover from a single collapse toggle showing
> all 6 rails at once into 6 **badges** (one per category+media_type) where exactly
> one list is visible at a time. See Decision Log #25 and §4.6.
>
> **Update (2026-07-25c):** fixed the forum query reusing a stale prior search when
> picking a Discover title; Discover now auto-hides its shown panel once any title
> is selected (search or Discover); added a floating scroll-to-top button. See
> Decision Log #26-29.

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
| 17 | Discover section: 6 independent rails (Trending/Popular/Top Rated × Movie/TV), collapsible, collapsed by default, state in `localStorage` | Always expanded; hides during search; combined movie+TV rails | User wants trending + multiple curated lists without cluttering the primary search flow |
| 18 | No "Best of a given year" category in v1 | Include with year param | User excluded it when selecting categories — YAGNI |
| 19 | Trending window fixed to `week`, not exposed as a toggle | Day; user-selectable | Less noisy for a tool not opened daily; matches confirmed choice |
| 20 | Single generic `GET /api/discover?category&media_type&page` endpoint | 6 separate routes; 1 compound multi-rail endpoint | Per-rail independent "Load more" and error handling rule out compound; generic route avoids 6x boilerplate |
| 21 | Server-side cache: plain in-process dict, TTL from new env var `DISCOVER_CACHE_TTL_SECONDS` (default 3600) | `cachetools.TTLCache`; no caching | Bounded, tiny key space needs no eviction policy; TTL configurable per user request without a new dependency |
| 22 | UI page size 10, mapped 2:1 onto TMDB's native 20-item pages | Match TMDB's 20 directly; separate TMDB call per UI page | Halves TMDB calls for consecutive "Load more" clicks while keeping cache keyed on the natural TMDB page |
| 23 | `TitleCard.jsx` extracted from `TitleList.jsx` for reuse in rails | Duplicate card markup in `DiscoverRail` | Keeps Discover and Search visually identical with one source of truth |
| 24 | Rails fetch only once their section is expanded (lazy mount) | Fetch all 6 on app load regardless of visibility | Zero wasted requests/TMDB quota for a collapsed, unused section |
| 25 | Discover reworked from a single collapse toggle (all 6 rails stacked) to **6 badges**, one visible list at a time; click active badge again to deselect; default none active; layout switched from horizontal poster rail to the same vertical grid as search results (`.title-list`); active badge persisted in `localStorage`. A badge's list stays mounted (hidden, not unmounted) once first viewed, so switching back doesn't re-fetch | Keep the original stacked-rails layout; unmount/remount on every switch | User wanted a more compact, focused browsing pattern; mount-once-hide-rest preserves the no-redundant-refetch property from Decision #21/#24 across badge switches, not just collapse/expand |
| 26 | Forum `raw_query` for a Discover-originated selection always uses the title's own name, ignoring any leftover `rawQuery` from an earlier unrelated search | Only fall back to the title's name when `rawQuery` is empty | Bug: a prior search's typed text (e.g. "batman") was leaking into an unrelated Discover pick's forum search; Decision #11's "raw typed text" intent only applies when that typed text is actually about the selected title |
| 27 | Discover's active badge (`active`) lifted from `DiscoverSection` to `App.jsx` as a controlled prop; App clears it (in-memory only, not persisted) whenever any title is selected — search or Discover — auto-hiding the shown panel | Keep `active` owned inside `DiscoverSection`; only auto-hide for Discover-originated selections | Selecting a title makes the streams area the focus; App is the only place that knows "a selection just happened" for both entry points. Not persisting the clear preserves the user's last explicitly-clicked badge choice across reloads (Decision #25) even though it's hidden for the rest of that session |
| 28 | Floating **scroll-to-top** button, bottom-right, appears past a scroll threshold (400px) | Always visible; a "back to top" link at the page bottom | Wizard flow can get long (results + forum links); a persistent affordance beats scrolling manually, and hiding it near the top avoids clutter when there's nothing to scroll past |
| 29 | "← Change title" restores whichever Discover badge was active before the selection that auto-hid it (Decision #27), via a `discoverActiveBeforeSelect` value captured at selection time | Leave it cleared; require re-clicking the badge | Bug: backing out via Change title left Discover looking empty even though the same badge's list was still cached underneath — restoring feels like undoing the selection, not a fresh Discover session |

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
    discover.py      # /api/discover (trending/popular/top-rated rails)
  services/
    tmdb.py          # search_multi(), external_ids(), trending(), popular(), top_rated()
    torrentio.py     # fetch_streams(), parse + build_magnet()
    forum.py         # search(), build_topic_url(), fetch_topic() + HTML parse
    discover_cache.py # in-process TTL cache for discover responses
  models.py          # Pydantic response schemas
  utils/magnet.py    # magnet builder, url-encoding helpers
tests/               # pytest — parsing/magnet/slug logic + fixtures
.env                 # TMDB_API_KEY, FORUM_BASE_URL default, DISCOVER_CACHE_TTL_SECONDS
config.json          # persisted forum base URL override (gitignored)
```

**Frontend layout (`frontend/`):** Vite React app — `api/client.js` (backend fetch
wrapper), `api/torrentio.js` (client-side Torrentio fetch/parse/magnet, mirrors the
backend service), `torrentioMode.js` (client/server toggle persisted in
`localStorage`), `discoverSectionState.js` (active Discover badge key persisted in
`localStorage`; read/written by `App.jsx`, which owns the `active` state — see
§4.6), `components/` (SearchBar, TitleList, TitleCard, SeasonEpisodePicker,
ResultTabs, TorrentioTab, ForumTab, ForumTopicRow, CopyButton, SettingsModal,
DiscoverSection, DiscoverRail, ScrollToTopButton), `App.jsx` orchestrating the wizard.

**Config precedence:** `config.json` value (if present & non-empty) → else `FORUM_BASE_URL` from `.env`. Re-read per request (cheap, always fresh).

**Stack:** Python 3.11+, FastAPI, httpx (async), BeautifulSoup4 (`lxml`), pydantic v2, pytest. Frontend: React 18, Vite, plain fetch.

### 4.1a UI Style & Layout

- **Visual style:** dark theme — dark background, blue accent, card-based rows,
  system font. Theme tokens live as CSS variables in `frontend/src/styles.css`.
- **Layout:** single-page vertical wizard. Sections reveal progressively as the user
  advances: search bar → **Discover badges (none active by default; auto-hides once
  any title is selected)** → title cards (with posters) → season/episode inputs (TV
  only) → tabbed results (Torrentio / Forum) → result rows with copy-magnet.
- **Feedback:** inline spinners per stage, dismissible per-source error banners,
  a **Retry** button on Torrentio failures, transient "Copied ✓" on magnet copy,
  a notice when a title has no IMDb ID, and a floating **scroll-to-top** button
  (bottom-right) once the page has scrolled past ~400px.
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

**Manual smoke checklist:** search → pick movie → copy magnet; pick TV with/without episode; expand forum topic; break base URL → see error banner; toggle Torrentio client/server in Settings (persists across reloads); on a Torrentio failure use **Retry** (including server→client flip then retry); click a Discover badge → its list loads, click a different badge → the first hides and the second loads, click the active badge again → it hides, click "Load more" on one, switch away and back → no re-fetch, reload the page → the active badge persists; kill network on one badge's category only → its error doesn't affect the others; type a search, get results, then pick a Discover title instead → forum search uses the Discover title's name, not the typed text; pick any title (search or Discover) → the Discover badge/panel hides, but reload the page and it's still restored; scroll down → a scroll-to-top button appears bottom-right, click it → smooth-scrolls to top.

### 4.6 Discover Section (Trending / Popular / Top Rated)

**Purpose:** browse curated TMDB lists without typing a search query, via badges
below the search bar. Exactly one list is visible at a time; no badge is active by
default.

**Categories & TMDB source (movie and TV each, 6 badges total):**
| Badge | TMDB endpoint |
|---|---|
| Trending Movies / TV | `/trending/movie/week`, `/trending/tv/week` |
| Popular Movies / TV | `/movie/popular`, `/tv/popular` |
| Top Rated Movies / TV | `/movie/top_rated`, `/tv/top_rated` |

**Backend:** `GET /api/discover?category={trending|popular|top_rated}&media_type={movie|tv}&page={n}`
(`page` ≥1, default 1) → `SearchResponse` (`{results: [TitleResult]}`, same shape as
`/api/search`). `app/services/tmdb.py` gains `trending()`/`popular()`/`top_rated()`,
built the same way as `search_multi`/`external_ids` (shared `get_with_retry` +
`_to_title_result`). Failures → 502, same as `/api/search`. Unaffected by the
frontend's badge rework below — the endpoint shape/contract hasn't changed.

**Caching:** `app/services/discover_cache.py` — an in-process
`dict[(category, media_type, tmdb_page), (timestamp, items)]`, with a per-key
`asyncio.Lock` so concurrent misses on the same key don't both hit TMDB. TTL is
`DISCOVER_CACHE_TTL_SECONDS` (`.env` var, default `3600`). Lost on backend restart;
single-worker assumption already applies (see `services/scheduler.py`).

**Pagination mapping:** UI page size is 10; TMDB's native page size is 20. The router
computes `tmdb_page = (ui_page - 1) // 2 + 1` and slices the correct half out of the
cached 20-item TMDB page, so two consecutive "Load more" clicks reuse one cached
TMDB call.

**Frontend:** `TitleCard.jsx` extracted from `TitleList.jsx` (shared poster/title/year
markup, used by both the search grid and Discover). `DiscoverRail.jsx` — one per
category+media_type, owns its own `{items, page, loading, error, hasMore}`, fetches
on mount, renders its items in the same vertical `.title-list` grid as search results
(not a poster rail), plus a centered "Load more" and a scoped `ErrorBanner`.
`DiscoverSection.jsx` renders 6 badges (`role="tablist"`); clicking one sets it
`active` and — the first time — adds it to an `everActive` set, so its `DiscoverRail`
mounts once and then just toggles a `discover-panel-hidden` CSS class on later
switches instead of unmounting, avoiding a re-fetch when the user switches back to
a badge they've already viewed. Clicking the active badge again clears `active`
(hides it, nothing shown) without removing it from `everActive`. The active badge
key is persisted via `discoverSectionState.js` (`localStorage`, mirrors
`torrentioMode.js`'s pattern); no badge is active on first load.

`active` is **owned by `App.jsx`**, not `DiscoverSection` — `DiscoverSection` is a
controlled component (`active` + `onToggleBadge` props; `everActive` stays internal,
since it's purely a rendering/caching concern). `App.jsx` clears `active` to `null`
(in-memory only — `setActiveDiscoverBadge` isn't called, so `localStorage` keeps
whatever the user last explicitly clicked) whenever any title is selected, from
either Discover or a search result, so the shown panel auto-hides once the user's
attention moves to the streams area; the badge's `DiscoverRail` stays mounted
underneath, so reopening it later is still instant. Clicking a card calls the same
`onSelect` handler `TitleList` already uses, tagged `{ fromDiscover: true }` so
`App.jsx` knows to use the title's own name for the forum `raw_query` instead of
whatever was last typed in the search bar (Decision #26) — identical downstream
flow otherwise (external-ids → season/episode picker for TV → streams).
