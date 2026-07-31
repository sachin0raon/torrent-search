// Where Comet is fetched from: 'client' (browser) or 'server' (backend).
// Persisted per-browser in localStorage. Mirrors torrentioMode.js — Comet
// reflects whatever Origin it's sent (permissive CORS), so client mode works
// the same way it does for Torrentio.
const KEY = 'cometMode';
const DEFAULT_MODE = 'client';

// localStorage.getItem is synchronous I/O; getCometMode() is called on every
// fetch, so cache the parsed value in memory. setCometMode keeps the cache in
// sync on our own writes; the `storage` listener invalidates it if another tab
// changes the value (same-tab writes never fire `storage`, only other tabs' do).
let cached;
window.addEventListener('storage', (e) => {
  if (e.key === KEY) cached = undefined;
});

export function getCometMode() {
  if (cached !== undefined) return cached;
  try {
    const v = localStorage.getItem(KEY);
    cached = v === 'server' || v === 'client' ? v : DEFAULT_MODE;
  } catch {
    cached = DEFAULT_MODE;
  }
  return cached;
}

export function setCometMode(mode) {
  cached = mode === 'server' ? 'server' : 'client';
  try {
    localStorage.setItem(KEY, cached);
  } catch {
    // localStorage unavailable (private mode / disabled) — ignore.
  }
}
