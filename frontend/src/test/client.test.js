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

  it('omits empty season/episode from streams URL', async () => {
    global.fetch = mockFetch(200, { torrentio: {}, forum: {} });
    await api.streams({ imdbId: 'tt1', mediaType: 'movie', rawQuery: 'x' });
    const calledUrl = global.fetch.mock.calls[0][0];
    expect(calledUrl).toContain('imdb_id=tt1');
    expect(calledUrl).toContain('media_type=movie');
    expect(calledUrl).not.toContain('season=');
    expect(calledUrl).not.toContain('episode=');
  });

  it('includes season/episode when provided', async () => {
    global.fetch = mockFetch(200, {});
    await api.streams({ imdbId: 'tt1', mediaType: 'tv', rawQuery: 'x', season: 2, episode: 5 });
    const calledUrl = global.fetch.mock.calls[0][0];
    expect(calledUrl).toContain('season=2');
    expect(calledUrl).toContain('episode=5');
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
