import { describe, it, expect, vi, beforeEach } from 'vitest';
import { streamer } from '../api/streamer.js';

function mockFetch(status, body) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  });
}

describe('streamer api', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('POSTs the magnet to create a session', async () => {
    global.fetch = mockFetch(200, { sessionId: 'abc', name: 'M', ready: true, files: [] });
    const s = await streamer.createSession('magnet:?xt=1');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/stream-api/sessions');
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body)).toEqual({ magnet: 'magnet:?xt=1' });
    expect(s.sessionId).toBe('abc');
  });

  it('GETs a session by id', async () => {
    global.fetch = mockFetch(200, { sessionId: 'abc' });
    await streamer.getSession('abc');
    expect(global.fetch.mock.calls[0][0]).toBe('/stream-api/sessions/abc');
  });

  it('throws the service error message and carries the status', async () => {
    global.fetch = mockFetch(409, { error: 'too many active streams, try again shortly' });
    await expect(streamer.createSession('magnet:?xt=1')).rejects.toMatchObject({
      message: 'too many active streams, try again shortly',
      status: 409,
    });
  });

  // --- docs/STREAMING.md §7: Active Streams panel (qBittorrent engine only) ---

  it('lists active torrents with the shared clientID header', async () => {
    global.fetch = mockFetch(200, [{ hash: 'AAA', name: 'M', progress: 0.5, paused: true }]);
    const list = await streamer.listActiveTorrents();
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/stream-api/torrents');
    expect(opts.headers['X-Client-Id']).toBeTruthy();
    expect(list[0].hash).toBe('AAA');
  });

  it('resumes a torrent by hash', async () => {
    global.fetch = mockFetch(204, null);
    await streamer.resumeTorrent('AAA');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/stream-api/torrents/AAA/resume');
    expect(opts.method).toBe('POST');
  });

  it('deletes a torrent by hash', async () => {
    global.fetch = mockFetch(204, null);
    await streamer.deleteTorrent('AAA');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/stream-api/torrents/AAA');
    expect(opts.method).toBe('DELETE');
  });

  it('moves a torrent to downloads', async () => {
    global.fetch = mockFetch(204, null);
    await streamer.moveToDownloads('AAA');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/stream-api/torrents/AAA/move-to-downloads');
    expect(opts.method).toBe('POST');
  });

  it('flushes every torrent for this browser', async () => {
    global.fetch = mockFetch(204, null);
    await streamer.flushTorrents();
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/stream-api/torrents');
    expect(opts.method).toBe('DELETE');
  });

  it('surfaces a 404 (engine does not support the panel) with its status', async () => {
    global.fetch = mockFetch(404, { error: 'not found' });
    await expect(streamer.listActiveTorrents()).rejects.toMatchObject({ status: 404 });
  });
});
