# Stream Stats — Design Document

## Understanding Summary

- **What:** A global floating button (fixed, top-left) that opens a modal showing live download stats for all active torrent sessions
- **Why:** Give users a quick glance at per-file download progress without leaving the current view
- **Who:** Same single-user audience as the app; must be mobile-friendly
- **Content:** Per-session card with torrent name + connected seeders count; per-file rows showing progress bar, percentage, and bytes downloaded / total
- **Scope:** All active sessions grouped by torrent name; no peer counts or download speed
- **Refresh:** Auto-poll every 5 seconds, only while the modal is open; pauses when closed
- **Modal:** Solid background (no transparency), mobile-friendly layout

## Assumptions

- Floating button is hidden when no sessions exist; visible once at least one session is active
- Expired/GC'd sessions shown greyed out with "Session expired" label rather than silently removed
- Stats endpoint failures preserve last known data and show a small `⚠` indicator; modal stays open
- Empty state shows "No active streams" message
- Download speed is not included (per-file bytes only)
- File progress is clamped to 100% on the frontend (anacrolix `BytesCompleted` can overshoot on dirty pieces)

## Decision Log

| # | Decision | Alternatives Considered | Reason |
|---|----------|------------------------|--------|
| 1 | Stats on demand (modal) | Always visible, hidden | Keeps StreamPanel uncluttered |
| 2 | Per-file breakdown only | Torrent-wide only, both | User only needs file-level granularity |
| 3 | All sessions grouped | Latest only, on-screen only | User may have multiple magnets open simultaneously |
| 4 | Poll only while modal is open | Always poll, manual refresh | Avoids wasteful background requests when not needed |
| 5 | 5s poll interval | 2s, manual refresh | Balances freshness vs. request noise |
| 6 | Add seeders per session header | Speed, peer count | User explicitly requested ConnectedSeeders |
| 7 | React Context for session registry | Module singleton, lift to App | Reactive by default, no prop drilling, minimal wiring |
| 8 | Clamp progress client-side | Server-side clamp | Frontend already formats the value; avoids extra Go logic |
| 9 | No new frontend unit tests | Add component tests | Matches existing project testing standard |

---

## Final Design

### Backend

**New endpoint:** `GET /stream-api/sessions/{id}/stats`

**Response shape:**
```json
{
  "sessionId": "abc123",
  "name": "Movie.2024.mkv",
  "seeders": 8,
  "files": [
    {
      "index": 0,
      "name": "Movie.2024.mkv",
      "size": 4718592000,
      "downloaded": 2218762240
    }
  ]
}
```

**Error responses:**
- `410 Gone` — session has been GC'd (consistent with existing session endpoint)

#### Go changes

**`internal/stream/client.go`**
- Add `TorrentStat` struct: `ConnectedSeeders int`
- Add `Stats() TorrentStat` to `Torrent` interface
- Add `BytesCompleted() int64` to `TorrentFile` interface
- Implement both on `anacrolixTorrent` and `anacrolixFile`

**`internal/stream/manager.go`**
- Add `FileProgress` struct: `Index`, `Name`, `Size`, `Downloaded`
- Add `SessionStats` struct: `SessionID`, `Name`, `Seeders`, `Files []FileProgress`
- Add `GetStats(id string) (SessionStats, bool)` — reads stats outside the manager mutex (anacrolix is internally thread-safe)

**`internal/stream/handlers.go`**
- Register `GET /stream-api/sessions/{id}/stats`
- Return `410 Gone` if session not found (matches existing convention)

**`internal/stream/fakes_test.go`**
- Add `Stats() TorrentStat` stub on `fakeTorrent` (returns zero value)
- Add `BytesCompleted() int64` stub on `fakeFile` (returns `int64(len(f.data))`)

---

### Frontend

#### New file: `src/sessionContext.js`

React Context with `register(sessionId, name)` and `unregister(sessionId)` API.
Holds a `Map<sessionId, { sessionId, name }>` in state.
Export `SessionProvider` and `useSessions` hook.

#### `src/main.jsx`

Wrap `<App />` in `<SessionProvider />`.

#### `src/api/streamer.js`

Add `getStats(id)` → `GET /stream-api/sessions/{id}/stats`.

#### `src/components/StreamPanel.jsx`

On session creation success: call `register(session.sessionId, session.name)`.
On `useEffect` cleanup (unmount): call `unregister(sessionId)`.

#### `src/App.jsx`

Read `sessions` from `useSessions()`.
Render floating button (`stats-fab`) fixed top-left when `sessions.size > 0`.
Maintain `showStats` boolean state to open/close `StatsModal`.

#### New file: `src/components/StatsModal.jsx`

- On open: immediately fetch stats for all sessions, then poll every 5s via `setInterval`
- On close / unmount: `clearInterval` to stop polling
- Local state: `Map<sessionId, { ...SessionStats, expired, error }>`
- On `410 Gone`: set `expired: true` on that session's entry
- On other error: set `error: true`, keep last known data
- Layout: mobile-first, full-width on small screens, max-width `480px` centred on desktop, solid background
- Per-session card:
  - Header: torrent name + `N seeds` chip (greyed + "Session expired" if expired; `⚠` if last poll errored)
  - Spinner if no data yet for that session
  - File rows: filename, progress bar, `47% · 2.1 GB / 4.4 GB`
- Empty state (no sessions): "No active streams"

#### Progress display formula (frontend)

```js
const pct = Math.min(100, Math.round((downloaded / size) * 100));
// display: `${pct}% · ${formatSize(downloaded)} / ${formatSize(size)}`
```

---

### Edge Cases

| Scenario | Behaviour |
|----------|-----------|
| Session GC'd mid-poll | 410 → card greyed, "Session expired" |
| Network error on poll | Keep last data, show `⚠` on card header |
| `downloaded > size` | Clamp to 100% on frontend |
| Session with 0 files | Card header only, no file rows |
| First poll not yet resolved | Spinner per session card |
| StreamPanel unmounts while modal open | `unregister` fires → card disappears |

---

### Testing

**Go unit (`manager_test.go`):** Add `TestGetStats` using fake stubs — verify `FileProgress` fields and expired-session `false` return.

**Go handler (`handlers_test.go`):** Add tests for happy path and 410 case on the new endpoint.

**Frontend:** Manual browser verification (matches existing project standard).
