// Client-side Torrentio fetch. Mirrors backend/app/services/torrentio.py so the
// browser can call Torrentio directly (from the user's residential IP), which
// sidesteps the datacenter-IP 403s a VPS gets behind Cloudflare. Torrentio sends
// `access-control-allow-origin: *`, so cross-origin browser fetches are allowed.
//
// Returns the same { ok, error, items } shape the backend SourceResult uses, so
// ResultTabs consumes it unchanged.

const TORRENTIO_BASE =
  'https://torrentio.strem.fun/sort=seeders|qualityfilter=480p/stream';

const REQUEST_TIMEOUT_MS = 15000; // overall budget across attempts (server deadline is 15s)

// Retry policy mirroring the backend (get_retry_settings): 3 attempts total with
// exponential backoff + jitter, capped. Same retryable set as the server.
const RETRY = { attempts: 3, baseMs: 300, capMs: 3000, jitterMs: 500 };

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Backoff before the next attempt: exponential growth off baseMs, capped, plus
// a random jitter in [0, jitterMs). `attempt` is the 0-based index just completed.
function backoffMs(attempt) {
  const exp = Math.min(RETRY.baseMs * 2 ** attempt, RETRY.capMs);
  return exp + Math.random() * RETRY.jitterMs;
}

const SEEDERS_RE = /👤\s*([\d,]+)/u;
const SIZE_RE = /💾\s*([^⚙️👤]+)/u;
const PROVIDER_RE = /⚙️\s*([^\n]+)/u;

// Match Python's urllib.parse.quote_plus so client- and server-built magnets are
// byte-identical: keep A-Za-z0-9_.-~, encode everything else, space -> '+'.
function quotePlus(s) {
  return encodeURIComponent(s)
    .replace(/[!'()*]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase())
    .replace(/%20/g, '+');
}

export function buildStreamUrl(imdbId, mediaType, season, episode) {
  if (mediaType === 'movie') return `${TORRENTIO_BASE}/movie/${imdbId}.json`;
  if (season != null && episode != null) {
    return `${TORRENTIO_BASE}/series/${imdbId}:${season}:${episode}.json`;
  }
  return `${TORRENTIO_BASE}/series/${imdbId}.json`;
}

export function parseStreamTitle(rawTitle) {
  const raw = rawTitle || '';
  const lines = raw.split('\n');
  const title = lines.length ? lines[0].trim() : '';

  let seeders = null;
  const sm = raw.match(SEEDERS_RE);
  if (sm) {
    const n = parseInt(sm[1].replace(/,/g, ''), 10);
    seeders = Number.isNaN(n) ? null : n;
  }

  const zm = raw.match(SIZE_RE);
  const size = zm ? zm[1].trim() || null : null;

  const pm = raw.match(PROVIDER_RE);
  const source = pm ? pm[1].trim() || null : null;

  return { title, seeders, size, source };
}

function trackerSources(sources) {
  if (!Array.isArray(sources)) return [];
  return sources.filter((s) => typeof s === 'string' && s.startsWith('tracker:'));
}

export function buildMagnet(displayTitle, infoHash, sources) {
  const parts = [`magnet:?dn=${quotePlus(displayTitle)}`, `xt=urn:btih:${infoHash}`];
  for (const src of trackerSources(sources)) parts.push(`tr=${quotePlus(src)}`);
  return parts[0] + parts.slice(1).map((p) => `&${p}`).join('');
}

export function streamsToItems(streams) {
  const items = [];
  for (const s of streams || []) {
    const infoHash = s.infoHash;
    if (!infoHash) continue;
    const parsed = parseStreamTitle(s.title || '');
    const filename = (s.behaviorHints && s.behaviorHints.filename) || '';
    items.push({
      title: parsed.title || filename,
      seeders: parsed.seeders,
      size: parsed.size,
      source: parsed.source,
      magnet: buildMagnet(parsed.title, infoHash, s.sources),
    });
  }
  return items;
}

function httpError(status) {
  const e = new Error(`upstream returned ${status}`);
  e.status = status;
  return e;
}

function formatError(e) {
  if (e?.name === 'AbortError') return 'timed out';
  if (e?.status) return e.message; // "upstream returned 403"
  if (e instanceof TypeError) return 'could not reach Torrentio (network or ad-blocker)';
  return e?.message || 'request failed';
}

// Retry transient failures (network error, 429, 5xx) with exponential backoff.
// 4xx like 403 is non-retryable and returned to the caller unchanged.
async function fetchJson(url, signal) {
  let lastErr;
  for (let attempt = 0; attempt < RETRY.attempts; attempt++) {
    try {
      const res = await fetch(url, { signal, headers: { Accept: 'application/json' } });
      if (res.status === 429 || res.status >= 500) {
        lastErr = httpError(res.status); // transient -> fall through to backoff/retry
      } else if (!res.ok) {
        throw httpError(res.status); // non-retryable (e.g. 403)
      } else {
        return await res.json();
      }
    } catch (e) {
      if (e?.name === 'AbortError' || e?.status) throw e; // abort / non-retryable status
      lastErr = e; // network error -> fall through to backoff/retry
    }
    // Wait before the next attempt (skip after the final one).
    if (attempt < RETRY.attempts - 1) await sleep(backoffMs(attempt));
  }
  throw lastErr;
}

// Fetch Torrentio streams from the browser. Shaped like the backend SourceResult.
export async function fetchStreams({ imdbId, mediaType, season, episode }) {
  if (!imdbId) {
    return { ok: false, error: 'No IMDb ID available for this title', items: [] };
  }
  const url = buildStreamUrl(imdbId, mediaType, season, episode);
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), REQUEST_TIMEOUT_MS);
  try {
    const data = await fetchJson(url, ctrl.signal);
    return { ok: true, error: null, items: streamsToItems(data.streams || []) };
  } catch (e) {
    return { ok: false, error: formatError(e), items: [] };
  } finally {
    clearTimeout(timer);
  }
}
