import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from '../api/client.js';

function mockFetch(status, body) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  });
}

describe('api client', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('builds search URL with query param', async () => {
    global.fetch = mockFetch(200, { results: [] });
    await api.searchTitles('the matrix');
    expect(global.fetch).toHaveBeenCalledWith(
      '/api/search?query=the+matrix',
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('omits empty season/episode from the torrentio URL', async () => {
    global.fetch = mockFetch(200, { ok: true, error: null, items: [] });
    await api.torrentio({ imdbId: 'tt1', mediaType: 'movie' });
    const calledUrl = global.fetch.mock.calls[0][0];
    expect(calledUrl).toContain('/api/torrentio?');
    expect(calledUrl).toContain('imdb_id=tt1');
    expect(calledUrl).toContain('media_type=movie');
    expect(calledUrl).not.toContain('season=');
    expect(calledUrl).not.toContain('episode=');
  });

  it('includes season/episode when provided', async () => {
    global.fetch = mockFetch(200, { ok: true, error: null, items: [] });
    await api.torrentio({ imdbId: 'tt1', mediaType: 'tv', season: 2, episode: 5 });
    const calledUrl = global.fetch.mock.calls[0][0];
    expect(calledUrl).toContain('season=2');
    expect(calledUrl).toContain('episode=5');
  });

  it('builds independent comet/meteor/forumSearch URLs', async () => {
    global.fetch = mockFetch(200, { ok: true, error: null, items: [] });
    await api.comet({ imdbId: 'tt1', mediaType: 'movie' });
    expect(global.fetch.mock.calls[0][0]).toContain('/api/comet?');

    await api.meteor({ imdbId: 'tt1', mediaType: 'movie' });
    expect(global.fetch.mock.calls[1][0]).toContain('/api/meteor?');

    await api.forumSearch({ rawQuery: 'the matrix' });
    expect(global.fetch.mock.calls[2][0]).toBe('/api/forum/search?raw_query=the+matrix');
  });

  it('builds discover URL with category/media_type/page', async () => {
    global.fetch = mockFetch(200, { results: [] });
    await api.discover({ category: 'trending', mediaType: 'movie', page: 2 });
    expect(global.fetch).toHaveBeenCalledWith(
      '/api/discover?category=trending&media_type=movie&page=2',
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('sends PUT with JSON body for config', async () => {
    global.fetch = mockFetch(200, { forum_base_url: 'https://x', source: 'config' });
    await api.setConfig('https://x');
    const [, opts] = global.fetch.mock.calls[0];
    expect(opts.method).toBe('PUT');
    expect(JSON.parse(opts.body)).toEqual({ forum_base_url: 'https://x' });
  });

  it('throws with backend detail on error responses', async () => {
    global.fetch = mockFetch(400, { detail: 'Base URL must be a valid http(s) URL' });
    await expect(api.setConfig('bad')).rejects.toThrow('valid http(s) URL');
  });
});
