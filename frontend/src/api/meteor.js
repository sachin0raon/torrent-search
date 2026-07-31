// Client-side Meteor fetch. Mirrors backend/app/services/meteor.py so the
// browser can call Meteor directly. Meteor sends
// `access-control-allow-origin: *`, so cross-origin browser fetches are
// allowed, same as Torrentio.
//
// Meteor packs its title/metadata into `description` (not `title`), first
// line prefixed "📄 ", with 💾 size and 📺 quality/resolution (stored in the
// shared `source` slot — there's no dedicated quality field on the shared
// item shape). Seeders (👤) are absent from most sample responses but are
// still matched opportunistically. Meteor's config already requests
// seeders-first sorting upstream, so no local sort is applied here.
//
// Returns the same { ok, error, items } shape the backend SourceResult uses.

const METEOR_BASE =
  'https://meteorfortheweebs.midnightignite.me/eyJjYWNoZWRPbmx5IjpmYWxzZSwic2tpcFJlbGVhc2VGaWx0ZXIiOnRydWUsInJlbW92ZVRyYXNoIjpmYWxzZSwicmVtb3ZlU2FtcGxlcyI6ZmFsc2UsImFsbG93QWR1bHQiOnRydWUsImV4Y2x1ZGUzRCI6ZmFsc2UsImVuYWJsZVNlYURleCI6ZmFsc2UsInNob3dZb3VyTWVkaWEiOmZhbHNlLCJtaW5TZWVkZXJzIjowLCJtYXhSZXN1bHRzIjowLCJtYXhQZXJSZXNvbHV0aW9uIjowLCJyZXNvbHV0aW9ucyI6WyI0ayIsIjEwODBwIiwiNzIwcCJdLCJwcmVmZXJyZWRMYW5ncyI6WyJlbiIsIm11bHRpIl0sImxhbmd1YWdlcyI6W10sImV4Y2x1ZGVkTGFuZ3MiOltdLCJyZXN1bHRGb3JtYXQiOlsidGl0bGUiLCJxdWFsaXR5Iiwic2l6ZSIsImF1ZGlvIl0sImxhbmd1YWdlRm9ybWF0IjoiY29kZXMiLCJzb3J0T3JkZXIiOlsic2VlZGVycyIsImNhY2hlZCIsInJlc29sdXRpb24iLCJxdWFsaXR5IiwibGFuZ3VhZ2UiLCJzaXplIiwic2VhZGV4IiwicGFjayJdLCJhbGxvd1AyUCI6ZmFsc2UsImV4Y2x1ZGVkU291cmNlcyI6W119/stream';

const REQUEST_TIMEOUT_MS = 15000;

const RETRY = { attempts: 3, baseMs: 300, capMs: 3000, jitterMs: 500 };

function sleep(ms, signal) {
  return new Promise((resolve, reject) => {
    const abortError = () => signal?.reason ?? new DOMException('Aborted', 'AbortError');
    if (signal?.aborted) return reject(abortError());
    const t = setTimeout(resolve, ms);
    signal?.addEventListener(
      'abort',
      () => {
        clearTimeout(t);
        reject(abortError());
      },
      { once: true },
    );
  });
}

function backoffMs(attempt) {
  const exp = Math.min(RETRY.baseMs * 2 ** attempt, RETRY.capMs);
  return exp + Math.random() * RETRY.jitterMs;
}

const SEEDERS_RE = /👤\s*([\d,]+)/u;
const SIZE_RE = /💾\s*([^📺👤🔊]+)/u;
const QUALITY_RE = /📺\s*([^\n]+)/u;

function quotePlus(s) {
  return encodeURIComponent(s)
    .replace(/[!'()*]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase())
    .replace(/%20/g, '+');
}

export function buildStreamUrl(imdbId, mediaType, season, episode) {
  if (mediaType === 'movie') return `${METEOR_BASE}/movie/${imdbId}.json`;
  if (season != null && episode != null) {
    return `${METEOR_BASE}/series/${imdbId}:${season}:${episode}.json`;
  }
  return `${METEOR_BASE}/series/${imdbId}.json`;
}

export function parseStreamTitle(rawDescription) {
  const raw = rawDescription || '';
  const lines = raw.split('\n');
  let title = lines.length ? lines[0].trim() : '';
  if (title.startsWith('📄')) title = title.replace(/^📄/, '').trim();

  let seeders = null;
  const sm = raw.match(SEEDERS_RE);
  if (sm) {
    const n = parseInt(sm[1].replace(/,/g, ''), 10);
    seeders = Number.isNaN(n) ? null : n;
  }

  const zm = raw.match(SIZE_RE);
  const size = zm ? zm[1].trim() || null : null;

  const qm = raw.match(QUALITY_RE);
  const source = qm ? qm[1].trim() || null : null;

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
  const seenMagnets = new Set();
  for (const s of streams || []) {
    const infoHash = s.infoHash;
    if (!infoHash) continue;
    const parsed = parseStreamTitle(s.description || '');
    const filename = (s.behaviorHints && s.behaviorHints.filename) || '';
    const magnet = buildMagnet(parsed.title, infoHash, s.sources);
    if (seenMagnets.has(magnet)) continue;
    seenMagnets.add(magnet);
    items.push({
      title: parsed.title || filename,
      seeders: parsed.seeders,
      size: parsed.size,
      source: parsed.source,
      magnet,
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
  if (e?.status) return e.message;
  if (e instanceof TypeError) return 'could not reach Meteor (network or ad-blocker)';
  return e?.message || 'request failed';
}

async function fetchJson(url, signal) {
  let lastErr;
  for (let attempt = 0; attempt < RETRY.attempts; attempt++) {
    try {
      const res = await fetch(url, { signal, headers: { Accept: 'application/json' } });
      if (res.status === 429 || res.status >= 500) {
        lastErr = httpError(res.status);
      } else if (!res.ok) {
        throw httpError(res.status);
      } else {
        return await res.json();
      }
    } catch (e) {
      if (e?.name === 'AbortError' || e?.status) throw e;
      lastErr = e;
    }
    if (attempt < RETRY.attempts - 1) await sleep(backoffMs(attempt), signal);
  }
  throw lastErr;
}

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
