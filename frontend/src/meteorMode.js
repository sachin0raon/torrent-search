// Where Meteor is fetched from: 'client' (browser) or 'server' (backend).
// Persisted per-browser in localStorage. Mirrors torrentioMode.js — Meteor
// sends `access-control-allow-origin: *`, so client mode works the same way
// it does for Torrentio.
const KEY = 'meteorMode';
const DEFAULT_MODE = 'client';

// localStorage.getItem is synchronous I/O; getMeteorMode() is called on every
// fetch, so cache the parsed value in memory. setMeteorMode keeps the cache in
// sync on our own writes; the `storage` listener invalidates it if another tab
// changes the value (same-tab writes never fire `storage`, only other tabs' do).
let cached;
window.addEventListener('storage', (e) => {
  if (e.key === KEY) cached = undefined;
});

export function getMeteorMode() {
  if (cached !== undefined) return cached;
  try {
    const v = localStorage.getItem(KEY);
    cached = v === 'server' || v === 'client' ? v : DEFAULT_MODE;
  } catch {
    cached = DEFAULT_MODE;
  }
  return cached;
}

export function setMeteorMode(mode) {
  cached = mode === 'server' ? 'server' : 'client';
  try {
    localStorage.setItem(KEY, cached);
  } catch {
    // localStorage unavailable (private mode / disabled) — ignore.
  }
}
