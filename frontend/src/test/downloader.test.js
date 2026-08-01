import { describe, it, expect, vi, beforeEach } from 'vitest';
import { downloader } from '../api/downloader.js';

function mockFetch(status, body) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  });
}

describe('downloader api', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('GETs status', async () => {
    global.fetch = mockFetch(200, { enabled: true });
    const s = await downloader.getStatus();
    expect(global.fetch.mock.calls[0][0]).toBe('/download-api/status');
    expect(s.enabled).toBe(true);
  });

  it('POSTs the magnet to create a download', async () => {
    global.fetch = mockFetch(200, { hash: 'abc', name: 'M', files: [] });
    const info = await downloader.createDownload('magnet:?xt=1');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/download-api/torrents');
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body)).toEqual({ magnet: 'magnet:?xt=1' });
    expect(info.hash).toBe('abc');
  });

  it('POSTs selected file indices', async () => {
    global.fetch = mockFetch(204, null);
    await downloader.selectFiles('abc', [0, 2]);
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/download-api/torrents/abc/select');
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body)).toEqual({ indices: [0, 2] });
  });

  it('GETs the download list', async () => {
    global.fetch = mockFetch(200, [{ hash: 'abc' }]);
    const list = await downloader.listDownloads();
    expect(global.fetch.mock.calls[0][0]).toBe('/download-api/torrents');
    expect(list).toEqual([{ hash: 'abc' }]);
  });

  it('GETs one download by hash', async () => {
    global.fetch = mockFetch(200, { hash: 'abc' });
    await downloader.getDownload('abc');
    expect(global.fetch.mock.calls[0][0]).toBe('/download-api/torrents/abc');
  });

  it('DELETEs a download', async () => {
    global.fetch = mockFetch(204, null);
    await downloader.deleteDownload('abc');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/download-api/torrents/abc');
    expect(opts.method).toBe('DELETE');
  });

  it('throws the service error message and carries the status', async () => {
    global.fetch = mockFetch(404, { error: 'torrent not found' });
    await expect(downloader.getDownload('missing')).rejects.toMatchObject({
      message: 'torrent not found',
      status: 404,
    });
  });
});
