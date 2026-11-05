// Thin fetch wrapper for the backend API. All calls are relative (/api/*) and
// proxied to the FastAPI backend by Vite in dev.
import { getCsrfToken } from '../auth/csrf.js';

const NO_AUTH_EVENT = ['/api/auth/login', '/api/auth/register', '/api/auth/me'];

export async function request(path, { method = 'GET', body, signal } = {}) {
  const opts = { method, signal, headers: {} };
  if (!['GET', 'HEAD'].includes(method) && getCsrfToken()) {
    opts.headers['X-CSRF-Token'] = getCsrfToken();
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
    const detail = (data && data.detail) || `Request failed (${res.status})`;
    const err = new Error(detail);
    err.status = res.status;
    if (res.status === 401 && !NO_AUTH_EVENT.includes(path.split('?')[0])) {
      window.dispatchEvent(new Event('auth:unauthorized'));
    }
    throw err;
  }
  return data;
}

const qs = (params) => {
  const usp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') usp.append(k, v);
  }
  return usp.toString();
};

export const api = {
  searchTitles: (query, signal) =>
    request(`/api/search?${qs({ query })}`, { signal }),

  externalIds: (mediaType, tmdbId, signal) =>
    request(`/api/external-ids?${qs({ media_type: mediaType, tmdb_id: tmdbId })}`, { signal }),

  tvSeasons: (tmdbId, signal) =>
    request(`/api/tv-seasons?${qs({ tmdb_id: tmdbId })}`, { signal }),

  torrentio: ({ imdbId, mediaType, season, episode }, signal) =>
    request(
      `/api/torrentio?${qs({ imdb_id: imdbId, media_type: mediaType, season, episode })}`,
      { signal },
    ),

  comet: ({ imdbId, mediaType, season, episode }, signal) =>
    request(
      `/api/comet?${qs({ imdb_id: imdbId, media_type: mediaType, season, episode })}`,
      { signal },
    ),

  meteor: ({ imdbId, mediaType, season, episode }, signal) =>
    request(
      `/api/meteor?${qs({ imdb_id: imdbId, media_type: mediaType, season, episode })}`,
      { signal },
    ),

  forumSearch: ({ rawQuery }, signal) =>
    request(`/api/forum/search?${qs({ raw_query: rawQuery })}`, { signal }),

  discover: ({ category, mediaType, page }, signal) =>
    request(
      `/api/discover?${qs({ category, media_type: mediaType, page })}`,
      { signal },
    ),

  forumTopic: (url, signal) =>
    request(`/api/forum/topic?${qs({ url })}`, { signal }),

  x1337Search: ({ q }, signal) =>
    request(`/api/1337x/search?${qs({ q })}`, { signal }),

  x1337Magnet: ({ path }, signal) =>
    request(`/api/1337x/magnet?${qs({ path })}`, { signal }),

  getConfig: (signal) => request('/api/config', { signal }),

  setConfig: (forumBaseUrl) =>
    request('/api/config', { method: 'PUT', body: { forum_base_url: forumBaseUrl } }),
};
