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

---

## 5. qBittorrent Engine (selectable download backend)

> Status: **Implemented** (2026-08-01). Extends §4 above.
> Adds a second, selectable BitTorrent download engine — **qBittorrent**, driven over
> its Web API — alongside the existing `anacrolix/torrent` engine. This is additive:
> the default engine and all existing anacrolix behavior (including the
> `QBitPeerSource` peer-accelerator) are unchanged.
>
> Reviewed via structured multi-agent design review (Skeptic/Challenger, Constraint
> Guardian, User Advocate, Integrator/Arbiter) — 21 decisions in the Decision Log,
> 2 objections explicitly rejected with rationale. Final disposition: **APPROVED**.

### 5.1 Understanding Summary

- **What:** An env-selectable second download engine. `STREAM_ENGINE=qbittorrent`
  routes torrent add/metadata/file-list/streaming through a running qBittorrent
  instance instead of the embedded anacrolix client. `STREAM_ENGINE` unset or
  `anacrolix` (default) is byte-for-byte identical to today.
- **Why:** qBittorrent (libtorrent) fetches metadata and finds peers dramatically
  faster than anacrolix's DHT cold-start — already proven by the existing
  `QBitPeerSource` peer-donor hack. This makes it usable as the real engine, not
  just a peer source, without losing the anacrolix path.
- **Who:** Personal tool, single VPS, no auth — unchanged.
- **How (high level):** A new `qbtClient`/`qbtTorrent`/`qbtFile` adapter
  (`streamer/internal/stream/qbt_client.go`) implements the existing
  `TorrentClient`/`Torrent`/`TorrentFile` interfaces from `client.go`, so
  `manager.go` and `handlers.go` require no changes. A new piece-aware
  `Reader` (`qbt_reader.go`) reads directly off disk from qBittorrent's
  `save_path`, blocking on `GetTorrentPieceStates` until the needed piece is
  downloaded. qBittorrent runs externally on the same host; its download
  directory is bind-mounted into the streamer container.
- **Non-goals:** No change to HTTP handlers' existing behavior/shape, nginx,
  player-link building, the FastAPI/React layers, or default (anacrolix) behavior.
  (`ErrLocalFileMissing`/`ErrTorrentGone`, both surfaced from `Reader.Read` rather
  than `OpenFile`, don't get a clean mapped HTTP status the way `handlers.go`'s
  existing `OpenFile`-error `switch` handles `ErrNotFound`/`ErrFileIndex` — see the
  correction in §5.8 — but neither changes that switch's existing behavior either.)
  No Docker Compose/Dockerfile changes — qBittorrent's own deployment is out of
  scope here.
- **Operational note — Cloudflare Tunnel / any reverse-proxy idle-timeout (found in
  production, 2026-08-01):** if this deployment sits behind Cloudflare Tunnel (or
  any CDN/tunnel/LB) rather than being reached directly, that layer's own idle-
  connection timeout applies *in addition to* `nginx.conf`'s `/stream/` timeouts
  (already generous — `proxy_read_timeout`/`proxy_send_timeout` `3600s`). Since the
  qBittorrent-engine reader can go silent (no bytes written) for as long as the
  current piece takes to download — which for large pieces can exceed such an
  external timeout — the outer layer may kill the connection first, regardless of
  nginx's own config, producing repeated client-side reconnect attempts. This is a
  deployment/infra consideration outside this repo's control, not something fixable
  via `nginx.conf`.
- **Accepted risk (extended, Decision #19):** §3's "fully open, no auth" accepted
  risk was scoped to bandwidth/abuse. The bind-mount this design relies on widens
  that to filesystem exposure — the streamer container gets read access to
  qBittorrent's entire `save_path`, not just this app's own torrents, on an
  unauthenticated, internet-facing service. Explicitly accepted, not architecturally
  mitigated (see §5.3 decision #19); operators with a qBittorrent instance shared
  with unrelated data should bind-mount a scoped subdirectory if tighter isolation
  matters to them.

### 5.2 Assumptions

1. Engine selection is a single env var (`STREAM_ENGINE`); default preserves all
   existing behavior exactly.
2. **Safety-critical:** `QBitPeerSource` is wired up **only** when
   `STREAM_ENGINE=anacrolix`. Running it against a qBittorrent-engine session would
   delete the real downloading torrent and its data (same infohash, same instance) —
   its "delete the probe torrent" cleanup can't distinguish a probe from the real thing.
3. The qBittorrent-engine adapter reuses the existing `STREAM_QBIT_HOST/USER/PASS`
   config rather than parallel vars, since only one of "peer accelerator" or "real
   engine" is ever active.
4. `STREAM_ENGINE=qbittorrent` fails fast at startup if login fails — this engine has
   no fallback once selected. Anacrolix mode keeps qBittorrent fully optional as today.
5. Infohash is parsed client-side from the magnet URI (hex or base32 `btih`), since
   `AddTorrentFromUrl` doesn't return it synchronously. `v2`/`btmh`-only magnets aren't
   supported (same limitation `QBitPeerSource` already has).
6. Torrents added by the qBittorrent engine are tagged with a category
   (`QBitCategory`, e.g. `tsa-stream-engine`); on every startup, torrents in that
   category are purged (data included) before serving traffic, cleaning up orphans
   from a prior crash/restart — the qBittorrent-engine equivalent of `WipeDownloadDir()`.
7. `sanitizeMagnet` is reused for both adapters — it's plain string normalization,
   cheap to apply universally even though its motivating panic was anacrolix-specific.
8. No dedicated stall/cancellation timeout on the piece-aware reader for the
   **external** no-seeder case; a blocked `Read` waiting on an unavailable piece
   behaves like anacrolix's reader does today (blocks until data arrives or the
   process is killed). Not a regression for that case — carried forward as-is rather
   than solved net-new. (The *self-inflicted* stall case — a file demoted out from
   under an open reader — is prevented by construction, not carried forward; see
   Decision #14.)
9. Existing fakes/tests (`fakes_test.go`, `manager_test.go`, `handlers_test.go`) need
   no changes; new qBittorrent-specific logic gets its own fakes and unit tests.
10. `AddTorrentFromUrlCtx`'s idempotency on a duplicate-hash add (does it reset
    progress? does it re-apply category/priority options to an existing torrent?) is
    **unverified against the real API** — treated as an explicit integration-test
    item (§5.10), not a fact baked into the design.

### 5.3 Decision Log

| # | Decision | Alternatives considered | Why chosen |
|---|----------|------------------------|------------|
| 1 | **Additive, env-selected** second engine — anacrolix stays default and fully intact | Hard replacement of anacrolix | User requirement: keep anacrolix as a fallback/option, not a one-way migration |
| 2 | qBittorrent runs **externally**, same host, reached via a **bind-mounted path** for direct disk reads | Bundle qBittorrent into the image via supervisord; add a qbittorrent service to this repo's compose | User already runs qBittorrent themselves; compose/Dockerfile changes out of scope |
| 3 | New adapter satisfies the **existing** `TorrentClient`/`Torrent`/`TorrentFile` interfaces | New parallel interface/abstraction for the qBittorrent path | Keeps `manager.go`/`handlers.go` untouched; proven pattern already used for the anacrolix adapter |
| 4 | **Per-session polling** for metadata-readiness and piece state (no shared fan-out poller) | A single Manager-wide poller that fans state out to subscribers | Max 5 concurrent sessions makes redundant polling overhead negligible; fan-out adds pub/sub complexity not justified at this scale (YAGNI) |
| 5 | File-priority-on-pick via an **optional `filePrioritizer` interface + type assertion** | Add `PrioritizeFile` as a required method on the shared `Torrent` interface (anacrolix no-ops it) | Matches the existing `peerAdder`/`AddPeers` convention already in the codebase; avoids leaking a qBittorrent-only concern into the shared interface |
| 6 | **Only the picked file downloads** (`SetFilePriority`) on a season pack | Download all files by default | Matches the lean-VPS/bandwidth philosophy already established for this feature |
| 7 | `QBitPeerSource` and `AddPeers` machinery **kept as-is**, gated to the anacrolix branch only | Remove as redundant now that qBittorrent can be the real engine | User requirement: anacrolix path must remain exactly as it is today, peer-accelerator included |
| 8 | `sanitizeMagnet` **kept and reused** for both adapters | Drop it / scope to anacrolix only | User requirement: don't remove it; also cheap and harmless to apply universally |
| 9 | Public tracker-list augmentation (`trackers.go`) **retained**, applied to whichever engine is active | Drop it now that qBittorrent has strong peer discovery on its own | Still a peer-discovery boost either way; the interface (`Torrent.AddTrackers`) already generalizes across engines |
| 10 | Startup orphan cleanup via a **qBittorrent category tag**, purged on every start | No cleanup; rely on qBittorrent's own retention | Mirrors today's `WipeDownloadDir()` clean-slate guarantee without risking torrents unrelated to this app in a shared qBittorrent instance |
| 11 | Piece-aware reader has **no dedicated stall/cancellation timeout** for the external no-seeder case | Thread `r.Context()` through `OpenFile`/`NewReader` to support early cancellation | Not a regression vs. anacrolix's current behavior; deferred rather than solving a pre-existing, non-blocking limitation now |

**Review pass (Skeptic/Challenger, multi-agent-brainstorming) — decisions 12–16:**

| # | Decision | Objection raised | Resolution & rationale |
|---|----------|-------------------|-------------------------|
| 12 | `QBitPeerSource`'s delete-probe cleanup checks the target torrent's category before calling `DeleteTorrentsCtx`; skips + logs a warning if it matches `QBitCategory`. `InjectPeers`'s goroutine is tracked via `Manager.wg` so shutdown waits for it. | The engine-selection switch (§5.5) only guards one process. Two overlapping containers during a rolling redeploy (old anacrolix+QBitPeerSource, new qbittorrent-engine) pointed at the same qBittorrent instance could still let the peer-accelerator delete the real engine's in-flight data — the safety invariant (Decision #2/#7) held only within a single process, not against the qBittorrent instance itself. | **Accepted.** The category check converts a potential cross-process data-loss race into a safe no-op with a visible log line, without changing `QBitPeerSource`'s behavior for its normal (non-overlapping) use case. Residual risk (a brief overlap window during redeploy) isn't eliminated by process topology alone, but data loss is. |
| 13 | The piece-aware reader bounds-checks `pieceIndex` against the returned `GetTorrentPieceStatesCtx` slice length (out-of-range → treat as not-ready, re-poll) and treats `ENOENT` on file open as "not ready yet" (retry/poll), not a hard error. | qBittorrent can report `PiecesNum > 0` before its piece-map is fully populated, and doesn't preallocate files by default — an unguarded index or an eager `os.Open` could panic or hard-fail on a freshly-added torrent, precisely the class of "hasn't touched a real qBittorrent instance" overconfidence in the original design. | **Accepted.** Both are cheap, purely defensive checks that convert a crash/hard-error into "not ready yet, keep polling" — the exact behavior the design already intended. |
| 14 | `qbtTorrent` tracks an open-reader refcount per file index; `PrioritizeFile` only demotes the previously-picked file once its refcount reaches zero (deferred demotion, not immediate). | Because file selection is exclusive (Decision #6) and there's no read-cancellation (Decision #11), switching files mid-session demotes the still-open previous file's priority to 0 — any reader still waiting on its pieces hangs **forever**, since the engine itself withdrew the data. This is a *new* failure mode, not the external no-seeder case Decision #11 accepted. | **Accepted, prevention over mitigation.** Deferred demotion means an in-use file is never starved by a switch — the only remaining stall class is the original external no-seeder case, so Decision #11's reasoning stays valid without adding a new timeout/config knob. |
| 15 | The `GotInfo` poller treats qBittorrent API errors (`GetTorrentPropertiesCtx` failures) as transient and keeps polling until the outer `MetadataTimeout` deadline (owned by `manager.go`'s `awaitInfo`), rather than exiting on first error. | Treating anacrolix's push-based `GotInfo()` and qBittorrent's polled version as interchangeable through the same `<-chan struct{}` type hides a real difference: the poller depends on a network call that can transiently fail, with no stated retry behavior — a Web API blip could turn into a full user-facing metadata-timeout with no anacrolix analogue. | **Accepted.** `awaitInfo`'s existing timeout is already the single source of truth for giving up; the poller just needs to not treat a transient error as fatal on its own. |
| 16 | Startup category-purge failure (§5.5/5.6) is fatal (`log.Fatalf`), consistent with Decision #4's fail-fast posture — not silently ignored. | No failure path was specified for the purge itself; if it errored, orphaned torrents/data could persist into the serving window, undermining the "clean slate" guarantee (Decision #10). | **Accepted.** Matches the existing fail-fast pattern already used for login failure; keeps "clean slate on startup" an actual guarantee rather than a best-effort. |

**Rejected objections:**

| Objection | Rejection rationale |
|---|---|
| `QBitPeerSource`'s probe torrents (in `qbt_peers.go`) aren't tagged with a category, so they're invisible to the new engine's startup purge — an orphan class outside both cleanup mechanisms. | Their lifecycle is already self-contained: bounded by `qbtMaxDuration` (2 min) and explicitly self-deleted in `qbt_peers.go`'s existing `defer`. Not a data-loss vector, and untouched by this design — out of scope for this review. |

**Review pass (Constraint Guardian, multi-agent-brainstorming) — decisions 17–19:**

| # | Decision | Objection raised | Resolution & rationale |
|---|----------|-------------------|-------------------------|
| 17 | `Torrent.Drop()` gains an `error` return (anacrolix adapter returns `nil`). `Manager` tracks sessions whose `Drop()` failed in a small pending-retry list, retried each GC tick until it succeeds, instead of discarding bookkeeping after one failed attempt. | No failure path was specified for a *runtime* delete (e.g. qBittorrent going down mid-session) — unlike Decision #16's startup-purge handling. A silently-swallowed failure leaves the torrent+data alive in qBittorrent, invisible to the Manager, uncleaned until the streamer container's *next restart* — potentially weeks on a long-lived container. Real unbounded-disk-growth risk, not cosmetic. | **Accepted.** Bounds the retry cadence to `GCInterval` (already 30s default) instead of "next restart," using the existing GC loop rather than new machinery. |
| 18 | Deferred demotion (Decision #14) is capped: if the previous file's open-reader refcount hasn't reached zero within `cfg.IdleTimeout` of a file switch, demote it anyway (log a warning). No new config — reuses the existing idle-timeout value. | A client disconnecting mid-blocked-`Read` (Decision #11's accepted no-cancellation case) means `Close()`/the refcount decrement never runs — Go can't interrupt a blocked synchronous `Read`. Deferred demotion then never fires for the abandoned file, which is strictly worse than anacrolix's equivalent leaked-goroutine case: here the leak also keeps an *unrelated* file actively downloading, indefinitely, specifically because of the qBittorrent-only exclusive-download optimization (Decision #6). | **Accepted.** Preserves Decision #14's intent (don't starve an active read) for the common case, while bounding the worst case to a value the system already exposes and the operator already understands, rather than reopening full read-cancellation (still out of scope per Decision #11). |
| 19 | The accepted-risk framing (§3's "fully open, no auth" — bandwidth/abuse) is explicitly extended: the bind-mount (Decision #2) gives the streamer container filesystem read access to qBittorrent's **entire** `save_path`, not just this app's own torrents, on an unauthenticated, internet-facing service. Documented as a widened risk category, not fixed architecturally. | Scoping the mount to a per-app subdirectory would require the user's own compose/volume setup, already out of scope per Decision #2 ("compose/Dockerfile changes out of scope here"). Silently letting the risk profile widen without saying so was the actual problem — the software layer resolves file paths only from server-side `GetFilesInformationCtx` data (no client-controlled traversal today), but the *capability* now exceeds what streaming strictly requires. | **Accepted as documentation.** No code change; recommend (not mandate) that operators bind-mount a scoped subdirectory if their qBittorrent instance holds data unrelated to this app. |

**Review pass (User Advocate, multi-agent-brainstorming) — decisions 20–21:**

| # | Decision | Objection raised | Resolution & rationale |
|---|----------|-------------------|-------------------------|
| 20 | `NewQBitClient` validates `QBitDownloadDir` exists/is readable at **startup** (fail-fast, same posture as Decision #4/#16). Separately, the reader distinguishes "piece not yet downloaded" (state != 2, keep polling — Decision #13) from "qBittorrent reports the piece downloaded but the local file still can't be opened" (a distinct sentinel error, surfaced as a specific stream-error response) — the latter can never resolve by waiting, unlike the former. | A `QBitRemoteRoot`/`QBitDownloadDir` mismatch is currently invisible until actual playback: session creation, metadata fetch, and the file list all succeed normally (qBittorrent has the real torrent info; only the *local* path is wrong), so the end-user sees a working file picker, taps a file, and gets an opaque player-side failure with zero context — a state that matches none of §4.8's documented error cases. The real cause is visible only in Go service logs an operator has no reason to check, since the symptom looks like a player/network problem. | **Accepted.** Startup validation catches the common typo'd-path case before any stream is attempted. The sentinel-error distinction (extending `handlers.go`'s existing error-mapping `switch`, the same pattern already used for `ErrNotFound`/`ErrFileIndex`) turns the unrecoverable residual case into a diagnosable response instead of a silent hang or generic failure — additive to the existing error-mapping pattern, not a change to its shape, so it doesn't conflict with §5.1's "no change to HTTP handlers" non-goal (that non-goal was about not altering existing documented behavior, not freezing all future error-plumbing). |
| 21 | README.md's Configuration table will be updated to include all `STREAM_QBIT_*` vars — both the pre-existing ones (`HOST`/`USER`/`PASS`, currently undocumented there despite existing in code today) and the new ones from §5.4 — as part of this design's implementation scope. | README is the operator's actual reference for every env var; STREAMING.md is treated as design/history, not a runbook. Without this, an operator flipping `STREAM_ENGINE=qbittorrent` on for the first time has no discoverable path to the two vars that are load-bearing and required (`QBitRemoteRoot`/`QBitDownloadDir`). | **Accepted.** This design inherits and enlarges a pre-existing doc gap; closing it fully (not just for the new vars) is in scope rather than deferred. |

**Rejected objections:**

| Objection | Rejection rationale |
|---|---|
| Switching files in a season pack under the qBittorrent engine leaves the old file's progress bar visibly climbing in the Active Streams modal for up to `IdleTimeout` (Decision #18), with no label explaining why. | Reviewer's own assessment: low severity, gracefully degrading (bounded, not accumulating), and progress bars behaving as progress bars isn't inherently confusing. A UI label/tooltip would require touching `frontend/src/`, which §5.1's Non-goals explicitly excludes — out of scope by an already-stated boundary, not an oversight. |

### 5.4 Configuration additions

| Var | Default | Purpose |
|---|---|---|
| `STREAM_ENGINE` | `anacrolix` | `anacrolix` (default, unchanged behavior) or `qbittorrent` |
| `STREAM_QBIT_REMOTE_ROOT` | — | Save-path root as qBittorrent itself sees it (e.g. `/data/downloads`); required when `STREAM_ENGINE=qbittorrent` |
| `STREAM_QBIT_DOWNLOAD_DIR` | — | Local filesystem root the streamer container sees for that same directory (bind mount target) |
| `STREAM_QBIT_CATEGORY` | `tsa-stream-engine` | qBittorrent category tag applied to torrents this engine adds; purged on every startup |
| `STREAM_QBIT_POLL_INTERVAL` | `1` (s) | Poll cadence for metadata-readiness and piece-state checks |

`STREAM_QBIT_HOST`/`STREAM_QBIT_USER`/`STREAM_QBIT_PASS` (existing, §4.6-adjacent) are
reused unchanged — they now serve either the peer-accelerator (anacrolix mode) or the
real engine (qbittorrent mode), never both at once.

**Documentation (Decision #21):** implementation includes adding every `STREAM_QBIT_*`
var above — plus the pre-existing `HOST`/`USER`/`PASS`, currently undocumented there —
to README.md's Configuration table, which is the operator's actual reference, not this
design doc.

### 5.5 Engine selection & wiring (`main.go`)

```go
var client stream.TorrentClient
switch cfg.Engine {
case "qbittorrent":
    c, err := stream.NewQBitClient(cfg.QBitHost, cfg.QBitUser, cfg.QBitPass,
        cfg.QBitRemoteRoot, cfg.QBitDownloadDir, cfg.QBitCategory, cfg.QBitPollInterval)
    if err != nil {
        log.Fatalf("streamer: qbittorrent engine unavailable: %v", err)
    }
    client = c // NewQBitClient purges any prior torrents tagged QBitCategory on
               // construction; purge failure is fatal (Decision #16), same as login failure
default:
    c, err := stream.NewAnacrolixClient(cfg.DownloadDir, cfg.TorrentPort, cfg.DHTStateFile)
    if err != nil {
        log.Fatalf("streamer: failed to start torrent client: %v", err)
    }
    client = c
    if cfg.QBitHost != "" {
        // existing QBitPeerSource wiring, unchanged — anacrolix branch only.
        // Its delete-probe cleanup now checks the target torrent's category before
        // deleting (skip + log if it matches QBitCategory) and its InjectPeers
        // goroutine is tracked via mgr's wait group — cross-process guard, Decision #12.
    }
}
mgr := stream.NewManager(cfg, client)
```

### 5.6 The qBittorrent adapter (`qbt_client.go`)

- **`NewQBitClient` startup validation (Decision #20):** in addition to login and the
  category purge (§5.5), validates that `QBitDownloadDir` exists and is readable —
  fail-fast on a typo'd path mapping before any stream is attempted, rather than
  surfacing only at playback time.
- **`qbtClient.AddMagnet`:** `sanitizeMagnet` → parse infohash from `xt=urn:btih:...`
  (hex as-is, base32 decoded to hex; no `btih` found → `ErrInvalidMagnet`) →
  `AddTorrentFromUrlCtx` with `{"category": QBitCategory, "sequentialDownload":
  "true", "firstLastPiecePrio": "true"}` (idempotent by hash) → return a
  `*qbtTorrent{hash, client}`.
- **`qbtTorrent.GotInfo()`:** channel closed by an internal poller once
  `GetTorrentPropertiesCtx` reports `PiecesNum > 0`. API errors during polling are
  treated as transient and retried on the next tick — the poller never exits early on
  a single failed call; `awaitInfo`'s outer `MetadataTimeout` remains the sole
  give-up point (Decision #15).
- **`qbtTorrent.Stats()`:** `TorrentProperties.Seeds/Peers/PeersTotal` →
  `TorrentStat{ConnectedSeeders, ActivePeers, TotalPeers}`.
- **`qbtTorrent.AddTrackers`:** flattens tiers, best-effort add via qBittorrent's
  trackers endpoint (log + ignore errors, matching today's posture).
- **`qbtTorrent.Drop() error`:** `DeleteTorrentsCtx(hash, true)`, returning any error
  (the shared `Torrent` interface's `Drop()` now returns `error`; the anacrolix
  adapter's implementation returns `nil` — see §5.9 for how `Manager` uses this).
- **`qbtTorrent.Files()`:** `GetFilesInformationCtx` → `[]TorrentFile`
  (`qbtFile{hash, name, size, client, downloadDir}`).
- Does **not** implement `AddPeers` (stays anacrolix/`QBitPeerSource`-only via
  type assertion) — see §5.7 for the analogous `filePrioritizer` pattern.

### 5.7 File-priority-on-pick

```go
type filePrioritizer interface{ PrioritizeFile(index int) }
```
Implemented only by `qbtTorrent`; `manager.go`'s `OpenFile` type-asserts and calls it
if present (same shape as the existing `peerAdder`/`makePeerAdder` pattern for
`AddPeers`) — zero-risk to the anacrolix path, no shared-interface pollution.
`PrioritizeFile` tracks the session's currently-prioritized index; a repeat call with
the same index no-ops (avoids an API call on every byte-range request); switching to a
different index promotes the new one to normal priority (`SetFilePriorityCtx`, 1)
immediately, so only one file actively downloads at a time in the common case.

**Deferred demotion (Decision #14):** `qbtTorrent` also tracks an open-reader refcount
per file index, incremented in `NewReader()` and decremented in the reader's `Close()`.
The *previous* index is only demoted to skip (`SetFilePriorityCtx`, 0) once its refcount
reaches zero — if a reader for the old file is still open when the user switches files,
demotion is deferred until that reader closes. This prevents a switch from starving an
in-flight read (which would otherwise hang forever, since there's no read-cancellation
— Decision #11 — and the engine itself would have withdrawn the data).

**Bounded grace period (Decision #18):** a client disconnecting mid-blocked-`Read`
means `Close()` never runs (Go can't interrupt a blocked synchronous `Read`), so the
refcount above never reaches zero — which would otherwise defeat deferred demotion
permanently for that file. `PrioritizeFile` timestamps each switch; if the previous
index's refcount hasn't hit zero within `cfg.IdleTimeout` of the switch, it's demoted
anyway (logged as a warning) rather than left downloading forever.

### 5.8 Piece-aware reader (`qbt_reader.go`)

- **Path mapping:** local path = `QBitDownloadDir + strings.TrimPrefix(SavePath,
  QBitRemoteRoot) + "/" + file.Name`. `SavePath` not prefixed by `QBitRemoteRoot` →
  explicit configuration error, not a silently wrong path.
- **`download_path` vs. `save_path` (found in production, 2026-08-01):** qBittorrent
  reports two possible locations — `save_path` (final destination) and, only when
  "Keep incomplete torrents in a different folder" is enabled, a separate
  `download_path` where files actually live while still downloading. The original
  design only used `save_path`, which fails immediately (piece confirmed downloaded,
  file not found at the mapped location → `ErrLocalFileMissing`) for any deployment
  using that common qBittorrent option. Fixed: `qbtFile.NewReader` builds a candidate
  path from `download_path` (if set) *and* `save_path`, both against the same
  `QBitRemoteRoot`/`QBitDownloadDir` pair, and tries `download_path` first (more
  likely correct for actively-streaming, not-yet-complete content). `qbtReader` tries
  each candidate in order at open time, self-healing across the download→complete
  transition since a fresh reader (each new HTTP range request) re-resolves from
  scratch. Operator implication: `QBitRemoteRoot`/the bind mount need to cover
  whichever of the two paths is relevant — ideally their common parent, so both
  resolve correctly across a torrent's lifetime, not just one.
- **Piece math:** at open time, `fileOffset = Σ(size of files before this index)`
  (from `GetFilesInformationCtx`, index order). For a read at file-relative offset
  `x`: `pieceIndex = (fileOffset + x) / PieceSize`.
- **Known limitation — whole-piece-only granularity (found in production,
  2026-08-01):** qBittorrent's Web API only reports piece state as a whole
  (`GetTorrentPieceStatesCtx` — downloaded or not), with no visibility into
  partial progress *within* a piece. The reader can therefore only unblock the
  player once an entire piece is fully downloaded and hash-verified, unlike
  anacrolix's `SetResponsive()` reader, which has direct in-process access to
  block-level state and can surface data in much smaller increments. For
  torrents with large pieces (e.g. 16 MiB, seen on a 22.5 GiB multi-file
  release), this can mean multi-second pauses between chunks, compounded by
  normal cold-start peer-availability variance for the very first piece —
  producing a noticeably chunkier playback start than the anacrolix engine on
  the same content. **Decision (accepted, no code change):** keep strict
  verified-only reads — never serve a piece before qBittorrent's own hash
  check confirms it — rather than peeking at on-disk bytes ahead of
  verification to reduce latency. The alternative (reading ahead of
  verification) would reduce start-up latency on large-piece torrents at the
  cost of occasionally serving bytes that later fail the hash check and get
  re-downloaded — a correctness/latency trade-off explicitly rejected in favor
  of the guarantee established in §5.8's read-clamping design (no unverified
  data ever reaches the player). Small/typical piece sizes (1–4 MiB, the
  common case) are largely unaffected.
- **Read:** clamp the returned length to never cross into an unconfirmed piece
  (`n = min(len(p), pieceEndByte - offset)`) — guarantees no zero-filled
  preallocated bytes are ever served as real data. Check a cached piece-state slice
  (refreshed in full via `GetTorrentPieceStatesCtx` — no per-piece endpoint exists);
  not ready → sleep `QBitPollInterval` and recheck; ready → `os.File.ReadAt`.
- **Piece-state bounds safety (Decision #13):** if `pieceIndex >= len(pieceStates)`
  (qBittorrent can report `PiecesNum > 0` before its piece-map is fully populated),
  treat as not-ready and re-poll — never index out of range. A `GetTorrentPieceStatesCtx`
  API error during this poll is treated as transient and retried on the next tick,
  consistent with Decision #15's precedent for the metadata poller — not a hard
  failure of the read.
- **Torrent deleted out-of-band (found in production, 2026-08-01):** treating every
  `GetTorrentPieceStatesCtx` error/not-ready result as transient (above) has a gap —
  if the torrent is deleted directly via qBittorrent's own UI (not through this app)
  while a reader is waiting on one of its pieces, it will never become "ready," and
  qBittorrent's piece-states endpoint doesn't reliably distinguish "not found" from
  "not ready yet" the way its properties endpoint does (`ErrTorrentNotFound`, a 404).
  Left unhandled, this retries forever. Fixed: every `existenceCheckEvery` (10) wait
  iterations, `Read` additionally calls `GetTorrentPropertiesCtx` and bails out with
  `ErrTorrentGone` if it gets back `ErrTorrentNotFound` specifically — any other error
  from that check is treated as inconclusive (assume it still exists), so a transient
  blip can't produce a false-positive abort. **Known residual limitation:** by the
  time this fires, `http.ServeContent` has already committed response headers/status
  (200/206) — same constraint that applies to `ErrLocalFileMissing` above, correcting
  an overstatement in the original Decision #20 write-up, which implied a clean
  mapped HTTP error response for both. In practice the connection is aborted (same
  class of symptom as any other mid-stream `Read` error), not resolved into a clean
  status code — but bailing out within a bounded ~`QBitPollInterval` × 10 window is a
  substantial improvement over an indefinite hang.
- **Bookkeeping cleanup on detection (found in production, 2026-08-01, follow-up):**
  detecting `ErrTorrentGone` inside `Read` only fixed the hang for *that* request —
  `Manager`'s own session bookkeeping had no idea anything was wrong, so a *retry* of
  the same session ID would repeat the identical ~10s detect-then-abort cycle rather
  than getting the existing clean `410 Gone` → "Restart stream" flow (which already
  exists for the *different* case of `Manager`'s own idle-GC removing a session).
  Fixed via a new optional interface, mirroring the `filePrioritizer`/`peerAdder`
  pattern: `goneNotifiable interface{ SetGoneCallback(func()) }`. `Manager.AddSession`
  wires `qbtTorrent`'s callback to `m.remove(s)` right after registering the session;
  `qbtReader.Read`, on detecting the torrent is gone, calls `torrent.notifyGone()`
  (guarded by `sync.Once` so concurrent in-flight requests don't double-invoke it)
  before returning `ErrTorrentGone`. Reuses the existing `Drop()`/cleanup path rather
  than a parallel one — safe because qBittorrent's `torrents/delete` endpoint is
  idempotent (`200 OK` even for an already-unknown hash, confirmed against the
  vendored client's implementation), so `Drop()` on an already-gone torrent succeeds
  cleanly rather than landing in the `pendingRemoval` retry list (Decision #17) forever.
- **`missingFiles` state (found in production, 2026-08-01, second follow-up):**
  `torrentExists` originally only checked "does the torrent entry exist at all"
  (`GetTorrentPropertiesCtx` + `ErrTorrentNotFound`) — it missed the case where the
  torrent entry still exists in qBittorrent but qBittorrent itself has determined the
  files are gone from disk (manual deletion outside qBittorrent, an unmounted volume,
  a failed recheck). In that case the piece the reader is waiting on would never
  become ready, and the old check would report "still exists," so the wait loop would
  never trigger `ErrTorrentGone`. Fixed by switching the check to `GetTorrentsCtx`
  (filtered by hash), which exposes `.State`: empty result → deleted entirely (same
  as before, just via a different endpoint call); `State == qbt.TorrentStateMissingFiles`
  → also treated as gone. Deliberately uses qBittorrent's own state rather than an
  independent disk-existence check on our side — an independent check would risk
  reintroducing the exact false-positive `ErrLocalFileMissing` was designed to avoid
  (a file not existing *yet* is normal mid-download; only qBittorrent's own piece/file
  tracking can reliably tell that apart from genuinely gone).
- **Lazy open, `ENOENT`-tolerant (Decision #13):** the underlying file is opened lazily
  on first read, not at construction. qBittorrent doesn't preallocate files by default,
  so a missing file on a freshly-added torrent is treated as "not ready yet" (sleep
  `QBitPollInterval`, retry the open) rather than a hard error.
- **Unrecoverable vs. transient missing-file distinction (Decision #20):** the case
  above is only "not ready yet" while the relevant piece-state is still `!= 2`. If
  qBittorrent reports the piece as downloaded (`== 2`) but the local file still can't
  be opened, that's a path-mapping misconfiguration, not a timing issue — polling
  would never resolve it. This returns a distinct sentinel error instead of continuing
  to retry; `handlers.go`'s `streamFile` maps it to a specific error response (same
  `switch`-on-sentinel-error pattern already used for `ErrNotFound`/`ErrFileIndex`),
  so the failure is diagnosable rather than an indefinite hang or an opaque
  player-side failure.
- **`SetReadahead`:** no-op — sequential-download + first/last-piece-priority (set at
  add-time) already drives qBittorrent's own prefetch.
- **Known limitation (carried forward, not new):** a `Read` blocked on a piece with no
  seeders stays blocked past client disconnect, identical to anacrolix's reader today.
  This is distinct from the self-inflicted demotion stall, which is prevented by
  construction rather than carried forward — see §5.7's deferred demotion.

### 5.9 Lifecycle & GC

Mostly unchanged in `manager.go` — this is the payoff of the interface boundary:
idle-GC (`lastRead`/`touchReader`), the `MaxActive` cap, explicit `DELETE`, and
`m.remove`'s `os.RemoveAll` cleanup (harmless no-ops for qBittorrent-engine sessions,
since that data never lived under `cfg.DownloadDir`) all work unchanged for either
engine. `m.remove` → `s.t.Drop()` → `DeleteTorrentsCtx(hash, true)` deletes qBittorrent
file data as part of that one call. On a streamer restart mid-session, orphaned
qBittorrent torrents are cleaned up at the *next* startup via the `QBitCategory` purge
(§5.2.6) — one restart-cycle delayed vs. anacrolix's instant `WipeDownloadDir()`, but
the same clean-slate guarantee (purge failure is fatal, Decision #16).

**Retry on runtime delete failure (Decision #17):** `Drop()` now returns an `error`
(§5.6). If it fails — e.g. qBittorrent itself is down or unreachable mid-session, not
just at startup — `m.remove` still removes the session from `Manager`'s bookkeeping
immediately (so it's not servable and doesn't count against `MaxActive`), but adds it
to a small pending-retry list instead of discarding it. Each subsequent GC tick
retries `Drop()` for anything in that list until it succeeds, bounding cleanup to
`GCInterval` (30s default) instead of leaving data orphaned in qBittorrent until the
streamer container's next restart.

**Cross-process guard (Decision #12):** `QBitPeerSource`'s delete-probe cleanup
(anacrolix branch only) now checks the target torrent's category before deleting,
skipping + logging if it matches `QBitCategory` — protects against a brief overlap
window during a redeploy where an outgoing anacrolix container and an incoming
qbittorrent-engine container are both live against the same qBittorrent instance.
`InjectPeers`'s goroutine is also tracked via `Manager.wg` so `Close()` waits for it.

### 5.10 Testing strategy

- **Unchanged:** `fakes_test.go`, `manager_test.go`, `handlers_test.go` — the engine
  choice and qBittorrent specifics don't leak into `Manager`'s tested behavior.
- **New unit tests** (pure logic, fake qBittorrent API client, no live instance):
  infohash parsing (hex/base32/invalid), path-mapping prefix substitution (including
  the misconfigured-prefix error case), piece-math/read-clamping arithmetic
  (table-driven), file-priority dedup and deferred-demotion-until-refcount-zero
  behavior (Decision #14, including the bounded-grace-period fallback, Decision #18),
  piece-state bounds safety and lazy-open `ENOENT`-tolerance (Decision #13), `GotInfo`
  poller resilience to a transient API error mid-poll (Decision #15), and `Manager`'s
  pending-retry list for a failed `Drop()` being retried and cleared on subsequent GC
  ticks (Decision #17, injected fake clock + a fake client that fails then succeeds);
  `NewQBitClient` failing fast when `QBitDownloadDir` doesn't exist/isn't readable, and
  the reader returning the distinct sentinel error (rather than retrying forever) when
  a piece is reported downloaded but the local file still can't be opened (Decision
  #20); `download_path`-before-`save_path` candidate ordering and the fallback across
  a torrent's download→complete transition (found in production, 2026-08-01); and the
  bounded existence-check that returns `ErrTorrentGone` when a torrent was deleted
  out-of-band, verified to *not* false-positive on a merely transient properties-check
  error (also found in production, 2026-08-01); and the `goneNotifiable` callback
  firing exactly once even under concurrent triggers, being a safe no-op when nothing
  registered one, and `Manager.AddSession` actually wiring it such that invoking it
  removes the session from bookkeeping (follow-up, 2026-08-01).
- **Safety-invariant test (Decision #12):** assert `QBitPeerSource` is never
  constructed when `cfg.Engine == "qbittorrent"`, and that its delete-probe path
  skips deletion (with a fake client) when the target torrent's category matches
  `QBitCategory`.
- **Fakes needed:** a fake for the qBittorrent HTTP client surface
  (`AddTorrentFromUrlCtx`, `GetTorrentPropertiesCtx`, `GetTorrentPieceStatesCtx`,
  `SetFilePriorityCtx`, `DeleteTorrentsCtx`, `GetFilesInformationCtx`).
- **Manual/integration** (extends the §4.9 checklist with a `STREAM_ENGINE=qbittorrent`
  pass): real add-magnet → metadata timing, real piece-state polling, real disk reads
  via the bind mount, startup category purge (including a forced-failure case, since
  it's now fatal), duplicate-hash add behavior against the real API (Assumption #10),
  and a regression check that `STREAM_ENGINE=anacrolix` (or unset) is still
  byte-for-byte today's behavior.
