# Additional Torrent Scrapers (Comet / Meteor) — Design Document

> Status: **Design approved** (brainstorming complete). Ready for implementation handoff.
> Date: 2026-07-31.
> Extends the base app described in [DESIGN.md](DESIGN.md). Adds two more
> Stremio-addon-style torrent scrapers alongside the existing Torrentio source.

---

## 1. Understanding Summary

- **What:** Add **Comet** and **Meteor** as two more torrent-scraper sources, each
  its own tab in the results view (tab order: **Comet, Meteor, Forum, Torrentio**),
  alongside the existing Torrentio and Forum sources.
- **Why:** More scraper coverage for the same title, using the same one-click-magnet
  pattern already established for Torrentio.
- **Who:** Same single local/VPS user as the base app — no change to that model.
- **Key discovery:** Comet and Meteor are *not* drop-in Torrentio clones. Both pack
  their title/metadata into a `description` field (not `title` like Torrentio) with
  different emoji markers, and neither reliably includes seeders for every entry.
  Both were initially assumed to differ in CORS support, but live testing showed all
  three sources are effectively client-fetchable (see Decision #4).
- **Key constraint:** the VPS deployment sees server-side friction hitting these
  providers directly (Comet: `405`; Meteor: `302`), which is the same class of
  problem that motivated Torrentio's existing client/server toggle.
- **Non-goals:** no cross-source dedup (unchanged from base app Decision #6); no new
  non-functional requirements (same personal/single-user scale, same timeouts).

---

## 2. Assumptions

1. Comet/Meteor's magnet building reuses `utils/magnet.build_magnet` unchanged;
   entries without a `sources` array (debrid-cached, no trackers) get `dn`+`xt`-only
   magnets — the same fallback Torrentio's tests already exercise.
2. Same 10s outbound timeout / retry policy (`get_retry_settings`) as existing
   sources — no new non-functional requirements for a personal-scale tool.
3. All four tabs are always shown (no per-tab enable/disable setting), same as today.
4. Base URLs and their base64-encoded config blobs (resolution filters, result
   format, etc.) are hardcoded constants, not user-configurable — matching the
   existing `TORRENTIO_BASE` convention. Revisit only if an instance goes down.

---

## 3. Decision Log

| # | Decision | Alternatives considered | Why chosen |
|---|----------|------------------------|------------|
| 1 | Comet and Meteor each get their **own tab**, not merged into Torrentio's | Merge all three into one deduplicated "Torrents" tab; single tab with a source picker | Keeps each source's independent error/retry state simple and visible; avoids needing a shared item-dedup/sort policy across sources with different data completeness |
| 2 | Tab order: **Comet, Meteor, Forum, Torrentio** | Alphabetical; Torrentio-first (legacy) | User-specified order |
| 3 | No cross-source dedup | Dedup by infoHash across tabs | Consistent with base app Decision #6 (Torrentio/Forum already don't dedup against each other) |
| 4 | All three torrent sources (Torrentio, Comet, Meteor) get a **symmetric client/server toggle**, each independent, defaulting to **client** | Comet server-only (no toggle); one shared toggle for all sources | Comet was initially assumed CORS-less (no `access-control-allow-origin` on a bare `curl -I`/HEAD request), but a real GET request with an `Origin` header showed Comet reflects the origin (permissive CORS) — confirmed further by a live in-browser `fetch()` returning 224 results. All three sources are therefore client-fetchable; VPS testing showing server-side friction for all three (Torrentio's known Cloudflare 403, Comet 405, Meteor 302) supports defaulting every one of them to client mode. Per-source (not shared) toggles were chosen so e.g. Meteor could later be flipped to server-only independently if needed. |
| 5 | Backend endpoints **fully split** per source — `/api/torrentio`, `/api/comet`, `/api/meteor`, `/api/forum/search` — replacing the bundled `/api/streams` (Approach B) | Approach A: additive-only, leave `/api/streams` (Torrentio+Forum) untouched and only add new endpoints for Comet/Meteor | User chose the fuller refactor. Also incidentally fixes two pre-existing bugs: Forum's Retry button was never wired in the main (non-`forumOnly`) tab view (`ResultTabs.jsx`'s tab switch passed `onRetry` only to `TorrentioTab`), and the existing "Retry" only ever re-fetched Torrentio+Forum together via one shared `/api/streams` call, never independently |
| 6 | Splitting endpoints removes the `skip_torrentio`-style query param entirely | Add matching `skip_comet`/`skip_meteor` params for parity | Once each source is its own endpoint, "client mode" simply means the frontend never calls that backend route at all — no backend-side mode branching needed for any of the three torrent sources |
| 7 | Backend service modules (`comet.py`, `meteor.py`) **mirror `torrentio.py`'s structure per source**, not a shared "stream addon" abstraction | Generic base module + per-source adapter for field name/emoji differences | Differences (which field carries the title, which emoji marks which metadata) are real per-source variance, not superficial — an abstraction would need per-source overrides anyway for only 3 sources; matches the codebase's existing convention of independent service modules (`torrentio.py`/`forum.py` don't share a base either) |
| 8 | Reuse the existing `TorrentioItem` Pydantic model unchanged for Comet and Meteor items | New `CometItem`/`MeteorItem` models | Normalized output shape (`title`, `seeders`, `size`, `source`, `magnet`) is identical across all three sources once parsed |
| 9 | **Comet-only** local sort: items ranked seeders-descending, with `None`-seeder entries stable-sorted to the end | Sort all three sources the same way in code; leave Comet unsorted | Torrentio's `TORRENTIO_BASE` already requests `sort=seeders` upstream and Meteor's config's `sortOrder` already starts with `seeders` — both are presumably already seeder-ordered from the source. Only Comet's config lacks any sort request, and only ~44/239 sample entries even carry a seeder count (the rest are debrid-cached), so unsorted entries would otherwise appear in an arbitrary scrape order |
| 10 | **Progressive per-tab loading**: each of the four sources fetches and resolves independently; no single combined "loading" gate | Wait for all four sources before showing any tab | A slow source (e.g. Comet, which scrapes many providers) shouldn't block a fast one (e.g. Torrentio) from being immediately browsable; matches having independent retry per source (Decision #5) |
| 11 | Generalized `TorrentTab`/`TorrentRow` component (parameterized by a `sourceLabel`) reused across Torrentio/Comet/Meteor in `ResultTabs.jsx`; `ForumTab` stays its own component | Copy-paste a `CometTab`/`MeteorTab` per source, mirroring the backend's one-module-per-source split | The three torrent sources share an identical rendered row shape (title/seeders/size/source, Stream button, copy-magnet) — only the fetch/parse logic differs, which already lives in separate per-source modules (Decision #7). Forum's row shape (`ForumTopicRow`) is genuinely different, so it keeps its own component |
| 12 | This design persisted as its **own file** (`docs/SCRAPERS.md`), linked from `DESIGN.md`, rather than folded into `DESIGN.md` directly | Fold into `DESIGN.md` as a new `§4.x` section + dated update note, like the Discover feature | User chose the separate-file precedent (mirroring `STREAMING.md`) over the Discover-style inline fold |

---

## 4. Final Design

### 4.1 Backend endpoints & file layout

Each source gets its own router file and endpoint, replacing the old bundled call:

| Router | Route | Notes |
|---|---|---|
| `routers/torrentio.py` | `GET /api/torrentio?imdb_id&media_type&season&episode` | Moved out of the old `streams.py` |
| `routers/comet.py` | `GET /api/comet?imdb_id&media_type&season&episode` | New |
| `routers/meteor.py` | `GET /api/meteor?imdb_id&media_type&season&episode` | New |
| `routers/forum.py` | `GET /api/forum/search?raw_query` (new, alongside existing `/api/forum/topic`) | Also returns `forum_base_updated` |

All four return the shared `SourceResult` model (`{ok, error, items}`); the forum
search route additionally returns `forum_base_updated`. `routers/streams.py` and the
bundled `StreamsResponse` model are removed — no fan-out (`asyncio.gather`) is needed
once nothing is bundled, since each router now owns exactly one outbound call.
Movie/TV addressing, the ~10s timeout, and the shared `get_with_retry` retry policy
carry over unchanged, just relocated per-router.

### 4.2 Backend service modules (parsing)

`services/comet.py` and `services/meteor.py` mirror `services/torrentio.py`'s shape
(`build_stream_url`, `parse_stream_title`, `streams_to_items`, `fetch_streams`), with
source-specific parsing:

- **Field parsed:** both use `description` (not `title`), first line prefixed
  `📄 ` (stripped before use as the display title).
- **Metadata emoji:** Comet — `👤` seeders / `💾` size / `🔎` provider (same pattern
  as Torrentio, different provider emoji). Meteor — `💾` size / `📺`
  quality-resolution / `🔊` audio; its parser still attempts a `👤` match
  (harmless no-op if absent) rather than assuming seeders are never present.
- **Item shape:** both reuse `TorrentioItem` unchanged (Decision #8).
- **Magnet building:** unchanged via `utils/magnet.build_magnet`; entries without
  `sources` get `dn`+`xt`-only magnets (existing fallback).
- **Dedup by magnet:** same `seen_magnets` pattern as Torrentio.
- **Comet-only sort:** `streams_to_items` sorts items seeders-descending, `None`
  last, stable otherwise (Decision #9).
- **Constants:** `COMET_BASE` / `METEOR_BASE` added to `config.py` (base host + the
  provided base64 config blob), matching `TORRENTIO_BASE`.

### 4.3 Frontend — API modules & Settings

- **Client-side fetch modules:** `api/comet.js` / `api/meteor.js`, mirroring
  `api/torrentio.js`'s existing shape (`buildStreamUrl`, `parseStreamTitle` with the
  source-specific rules from §4.2, `buildMagnet`, retry-with-backoff,
  `fetchStreams()`) — used when that source's mode is `client`.
- **Backend-call wrappers:** `api/client.js` gains `torrentio()`, `comet()`,
  `meteor()`, `forumSearch()`, replacing the old single `streams()` call.
- **Mode toggles:** new `cometMode.js` / `meteorMode.js`, each an exact copy of
  `torrentioMode.js`'s localStorage-backed getter/setter (own key, own default
  `'client'`). Forum gets no mode toggle — it stays backend-only (HTML scraping).
- **SettingsModal:** gains two more independent toggle rows (Comet, Meteor)
  alongside the existing Torrentio one.

### 4.4 Frontend — App.jsx state & ResultTabs rendering

`App.jsx`'s single `streams` object + single `loading==='streams'` flag become
**per-source state**: `comet`, `meteor`, `torrentio`, `forum` each get their own
`{ok, error, items, loading}` slice. Selecting a title (or submitting season/episode)
kicks off four independent async calls — no `Promise.all`/gather — each updating
only its own slice as it resolves (Decision #10). Each source gets its own retry
function (`retryComet()`, `retryMeteor()`, `retryTorrentio()`, `retryForum()`)
replacing the old single `retryStreams()`. The existing "no `imdb_id` → skip, show
notice" behavior ports over per-source for the three torrent sources; Forum is
unaffected since it never needed one.

In `ResultTabs.jsx`, `TorrentioTab`/`TorrentioRow` become a generic
`TorrentTab`/`TorrentRow` parameterized by `sourceLabel` (Decision #11), reused for
Comet, Meteor, and Torrentio, each with its own `{result, onRetry, retrying}`.
`ForumTab` keeps its own component but now always receives `onRetry`/`retrying` in
the main tabbed view, wired to `retryForum()` — closing the pre-existing gap
(Decision #5). Tabs render in order: Comet, Meteor, Forum, Torrentio, each with its
own count badge reflecting that source's own `items.length` once resolved.
`forumOnly` mode simplifies to rendering only the Forum tab against `retryForum()`
— no more separate `searchForumOnly` code path duplicating fetch logic.

### 4.5 Error handling & edge cases

| Case | Behavior |
|---|---|
| Comet/Meteor outbound timeout (~10s) | That tab's `error: "timed out"`, independent of the others |
| Non-2xx from Comet/Meteor (403/405/etc.) | `"upstream returned {status}"`, same as Torrentio today |
| Client-mode network/CORS failure | Same `formatError()` mapping as `api/torrentio.js` today |
| Malformed/empty title in Comet or Meteor's `description` | Fallback to `behaviorHints.filename`; seeders shown as `—` when absent |
| Comet entry with no `sources` array (debrid-cached) | Magnet is `dn`+`xt` only — existing fallback |
| No `imdb_id` for the title | Comet/Meteor/Torrentio tabs independently show "No IMDb ID available"; Forum still runs |
| TV season/episode not yet chosen | All three torrent-source fetches wait for the picker, same gating as today, extended to all three |
| Mode flipped in Settings mid-session, then Retry clicked | Retry reads the mode at click time and re-runs in whichever mode is currently selected for that source |

### 4.6 Testing strategy

**Backend (pytest):** `tests/test_comet.py` / `tests/test_meteor.py` mirroring
`tests/test_torrentio.py` — parsing fixtures for a normal entry (seeders/size/source
present), a debrid-cached entry (no `sources`, no seeders), a malformed/empty title,
and magnet dedup across duplicate infoHashes. Comet's tests additionally cover the
seeders-descending sort (`None` entries pushed to the end, stable order). Router
tests (`TestClient` + mocked `httpx`) for all four routes: success, upstream error
surfaced as `SourceResult(ok=False)` (not a 500), and missing-`imdb_id` skip
behavior for the three torrent routes. Real sample responses captured during design
(the `tt4357198` Comet/Meteor payloads) saved as
`tests/fixtures/comet_streams.json` / `meteor_streams.json`.

**Frontend (Vitest + RTL):** `api/comet.js`/`api/meteor.js` tests mirroring
`api/torrentio.js`'s suite (parsing, retry/backoff, `formatError` mapping).
`ResultTabs` tests covering the generalized `TorrentTab` across all three torrent
sources (correct per-source labels/counts) and regression coverage confirming
`ForumTab`'s retry now fires in the main (non-`forumOnly`) view. `App.jsx` tests
covering progressive resolution (one source's slice updates independently of others
still loading) and that Retry on one tab only re-fetches that source.

**Manual smoke checklist additions:** Comet/Meteor rows support Stream + Copy magnet
identically to Torrentio; each of the three client/server toggles persists
independently across reload; Comet's list is visibly seeders-sorted (unknown-seeder
entries last); retrying one tab doesn't reset the others' already-loaded results.
