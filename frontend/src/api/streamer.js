// Thin fetch wrapper for the Go streaming service (/stream-api/*), proxied by
// nginx to the localhost Go process. Errors carry the service's {error} message.
import { getDownloadClientId } from '../downloadClientId.js';

async function request(path, { method = 'GET', body, signal, withClientId = false } = {}) {
  const opts = { method, signal, headers: {} };
  if (withClientId) {
    opts.headers['X-Client-Id'] = getDownloadClientId();
  }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  let data = null;
  try {
    data = await res.json();
  } catch {
    data = null;
  }
  if (!res.ok) {
    const detail = (data && data.error) || `Request failed (${res.status})`;
    const err = new Error(detail);
    err.status = res.status;
    throw err;
  }
  return data;
}

export const streamer = {
  // Add a magnet and wait for its metadata; returns
  // { sessionId, name, ready, files: [{ index, path, size, streamable }] }.
  createSession: (magnet, signal) =>
    request('/stream-api/sessions', { method: 'POST', body: { magnet }, signal }),

  // Poll an existing session (e.g. while metadata is still arriving).
  getSession: (id, signal) => request(`/stream-api/sessions/${id}`, { signal }),

  // Fetch live stats for a session: seeders + per-file download progress.
  // Throws with err.status === 410 if the session has been GC'd.
  getStats: (id, signal) => request(`/stream-api/sessions/${id}/stats`, { signal }),

  // --- qBittorrent-engine-only Active Streams panel (docs/STREAMING.md §7) ---
  // Every call below 404s when the active engine doesn't support it (i.e.
  // STREAM_ENGINE=anacrolix) — callers should treat err.status === 404 the
  // same way downloader.js's callers treat a disabled Download Manager.
  // Scoped to this browser via the same clientID as the Download Manager
  // (getDownloadClientId — reused deliberately, not a parallel identity).

  // Every torrent (active + paused) in the streaming category, scoped to
  // this browser: [{ hash, name, progress, size, downloaded, paused }].
  listActiveTorrents: (signal) =>
    request('/stream-api/torrents', { signal, withClientId: true }),

  // Explicitly resumes a paused torrent (a no-op if it's already
  // complete/active — nothing left to resume).
  resumeTorrent: (hash, signal) =>
    request(`/stream-api/torrents/${hash}/resume`, { method: 'POST', signal, withClientId: true }),

  // Deletes a torrent immediately, skipping the retention grace period.
  deleteTorrent: (hash, signal) =>
    request(`/stream-api/torrents/${hash}`, { method: 'DELETE', signal, withClientId: true }),

  // Hands the torrent to the persistent Download Manager (recategorize, no
  // re-download) so it's kept permanently instead of expiring.
  moveToDownloads: (hash, signal) =>
    request(`/stream-api/torrents/${hash}/move-to-downloads`, { method: 'POST', signal, withClientId: true }),

  // Deletes every torrent in the streaming category belonging to this browser.
  flushTorrents: (signal) =>
    request('/stream-api/torrents', { method: 'DELETE', signal, withClientId: true }),
};
