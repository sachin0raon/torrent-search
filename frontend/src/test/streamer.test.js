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
});
