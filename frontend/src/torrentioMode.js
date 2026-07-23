// Where Torrentio is fetched from: 'client' (browser, user's residential IP) or
// 'server' (backend). Persisted per-browser in localStorage. Default is 'client'
// so a VPS deployment blocked by Cloudflare (datacenter-IP 403) works out of the
// box for most visitors; flip DEFAULT_MODE to 'server' to reverse that.
const KEY = 'torrentioMode';
const DEFAULT_MODE = 'client';

export function getTorrentioMode() {
  try {
    const v = localStorage.getItem(KEY);
    return v === 'server' || v === 'client' ? v : DEFAULT_MODE;
  } catch {
    return DEFAULT_MODE;
  }
}

export function setTorrentioMode(mode) {
  try {
    localStorage.setItem(KEY, mode === 'server' ? 'server' : 'client');
  } catch {
    // localStorage unavailable (private mode / disabled) — ignore.
  }
}
