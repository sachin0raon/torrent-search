// A random ID that scopes the Downloads modal (see DownloadsModal.jsx) to
// this browser instead of showing every torrent ever added to the shared
// qBittorrent backend. Persisted per-browser in localStorage — not
// sessionStorage — so closing a tab doesn't make your own in-flight
// downloads disappear from the list.
//
// This is a visibility/UX scope, not authentication: the header is
// forgeable/shareable like any client-supplied ID, so it doesn't isolate
// mutually untrusted users, only avoids one browser's downloads cluttering
// another's.
const KEY = 'downloadClientId';

let cached;

export function getDownloadClientId() {
  if (cached) return cached;
  try {
    let v = localStorage.getItem(KEY);
    if (!v) {
      v = crypto.randomUUID();
      localStorage.setItem(KEY, v);
    }
    cached = v;
  } catch {
    // localStorage unavailable (private mode / disabled) — fall back to an
    // in-memory ID so requests within this page load still carry a
    // consistent tag, even though it won't persist across reloads.
    cached = cached || crypto.randomUUID();
  }
  return cached;
}
