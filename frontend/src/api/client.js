// Thin fetch wrapper for the backend API. All calls are relative (/api/*) and
// proxied to the FastAPI backend by Vite in dev.

async function request(path, { method = 'GET', body, signal } = {}) {
  const opts = { method, signal, headers: {} };
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
    throw new Error(detail);
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

  streams: ({ imdbId, mediaType, rawQuery, season, episode }, signal) =>
    request(
      `/api/streams?${qs({
        imdb_id: imdbId,
        media_type: mediaType,
        raw_query: rawQuery,
        season,
        episode,
      })}`,
      { signal },
    ),

  discover: ({ category, mediaType, page }, signal) =>
    request(
      `/api/discover?${qs({ category, media_type: mediaType, page })}`,
      { signal },
    ),

  forumTopic: (url, signal) =>
    request(`/api/forum/topic?${qs({ url })}`, { signal }),

  getConfig: (signal) => request('/api/config', { signal }),

  setConfig: (forumBaseUrl) =>
    request('/api/config', { method: 'PUT', body: { forum_base_url: forumBaseUrl } }),
};
