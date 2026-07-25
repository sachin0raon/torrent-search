// Which Discover badge (if any) is currently active — a "category-mediaType"
// key, or null when none is selected. Persisted per-browser in localStorage so
// the choice survives reloads. No badge active by default.
const KEY = 'discoverActiveBadge';

export function getActiveDiscoverBadge() {
  try {
    return localStorage.getItem(KEY) || null;
  } catch {
    return null;
  }
}

export function setActiveDiscoverBadge(key) {
  try {
    if (key) localStorage.setItem(KEY, key);
    else localStorage.removeItem(KEY);
  } catch {
    // localStorage unavailable (private mode / disabled) — ignore.
  }
}
