// Client-side Comet fetch. Mirrors backend/app/services/comet.py so the
// browser can call Comet directly. Comet reflects whatever Origin it's sent
// (confirmed live: a GET with an Origin header gets back
// `access-control-allow-origin: <that origin>`), so cross-origin browser
// fetches are allowed, same as Torrentio.
//
// Comet packs its title/metadata into `description` (not `title`), first
// line prefixed "📄 ", with 👤 seeders / 💾 size / 🔎 provider. Many entries
// (debrid-cached) carry no seeders/sources at all, so results are sorted
// seeders-descending with unknown (null) seeders last.
//
// Returns the same { ok, error, items } shape the backend SourceResult uses.

const COMET_BASE =
  'https://comet.feels.legal/eyJtYXhSZXN1bHRzUGVyUmVzb2x1dGlvbiI6MCwibWF4U2l6ZSI6MCwiY2FjaGVkT25seSI6ZmFsc2UsInNvcnRDYWNoZWRVbmNhY2hlZFRvZ2V0aGVyIjp0cnVlLCJyZW1vdmVUcmFzaCI6ZmFsc2UsInJlc3VsdEZvcm1hdCI6WyJ0aXRsZSIsInNlZWRlcnMiLCJzaXplIiwidHJhY2tlciJdLCJkZWJyaWRTZXJ2aWNlcyI6W10sImVuYWJsZVRvcnJlbnQiOnRydWUsImRlZHVwbGljYXRlU3RyZWFtcyI6ZmFsc2UsInNjcmFwZURlYnJpZEFjY291bnRUb3JyZW50cyI6ZmFsc2UsImRlYnJpZFN0cmVhbVByb3h5UGFzc3dvcmQiOiIiLCJsYW5ndWFnZXMiOnsicmVxdWlyZWQiOltdLCJhbGxvd2VkIjpbXSwiZXhjbHVkZSI6W10sInByZWZlcnJlZCI6W119LCJyZXNvbHV0aW9ucyI6eyJyNTc2cCI6ZmFsc2UsInI0ODBwIjpmYWxzZSwicjM2MHAiOmZhbHNlLCJyMjQwcCI6ZmFsc2V9LCJvcHRpb25zIjp7InJlbW92ZV9yYW5rc191bmRlciI6LTEwMDAwMDAwMDAwLCJhbGxvd19lbmdsaXNoX2luX2xhbmd1YWdlcyI6ZmFsc2UsInJlbW92ZV91bmtub3duX2xhbmd1YWdlcyI6ZmFsc2V9fQ==/stream';

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
const SIZE_RE = /💾\s*([^🔎👤]+)/u;
const PROVIDER_RE = /🔎\s*([^\n]+)/u;

function quotePlus(s) {
  return encodeURIComponent(s)
    .replace(/[!'()*]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase())
    .replace(/%20/g, '+');
}

export function buildStreamUrl(imdbId, mediaType, season, episode) {
  if (mediaType === 'movie') return `${COMET_BASE}/movie/${imdbId}.json`;
  if (season != null && episode != null) {
    return `${COMET_BASE}/series/${imdbId}:${season}:${episode}.json`;
  }
  return `${COMET_BASE}/series/${imdbId}.json`;
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
  // Comet's config has no upstream sort request (unlike Torrentio/Meteor):
  // known-seeder items descending, unknown (null) last, stable.
  items.sort((a, b) => {
    if (a.seeders == null && b.seeders == null) return 0;
    if (a.seeders == null) return 1;
    if (b.seeders == null) return -1;
    return b.seeders - a.seeders;
  });
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
  if (e instanceof TypeError) return 'could not reach Comet (network or ad-blocker)';
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
