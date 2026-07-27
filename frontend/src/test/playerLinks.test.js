import { describe, it, expect } from 'vitest';
import { buildStreamUrl, playerLinks, baseName, PLAYERS } from '../playerLinks.js';

describe('playerLinks', () => {
  it('builds an absolute same-origin stream URL from the given origin', () => {
    const url = buildStreamUrl('sess1', 3, 'The Movie.mkv', 'https://host.tld');
    expect(url).toBe('https://host.tld/stream/sess1/3/The%20Movie.mkv');
  });

  it('encodes special characters in the filename', () => {
    const url = buildStreamUrl('s', 0, 'a&b#c.mp4', 'https://h');
    expect(url).toBe('https://h/stream/s/0/a%26b%23c.mp4');
  });

  it('produces one deep link per known player in the specified formats', () => {
    const links = playerLinks('https://h/stream/s/0/f.mkv', 'f.mkv');
    expect(links).toHaveLength(PLAYERS.length);
    const byId = Object.fromEntries(links.map((l) => [l.id, l.href]));
    // VLC: scheme stripped from the URL.
    expect(byId.vlc).toBe('vlc://h/stream/s/0/f.mkv');
    // nPlayer: full URL with scheme.
    expect(byId.nplayer).toBe('nplayer-https://h/stream/s/0/f.mkv');
    // MX Player: intent URI with the file name as the title.
    expect(byId.mx).toBe(
      'intent:https://h/stream/s/0/f.mkv#Intent;package=com.mxtech.videoplayer.ad;S.title=f.mkv;end',
    );
  });

  it('strips http as well as https for VLC', () => {
    const [vlc] = playerLinks('http://h:8000/stream/s/0/f.mp4', 'f.mp4');
    expect(vlc.href).toBe('vlc://h:8000/stream/s/0/f.mp4');
  });

  it('URL-encodes the MX title so special chars do not break the intent', () => {
    const links = playerLinks('https://h/stream/s/0/f.mkv', 'A; B #1.mkv');
    const mx = links.find((l) => l.id === 'mx').href;
    expect(mx).toContain('S.title=A%3B%20B%20%231.mkv');
    expect(mx.endsWith(';end')).toBe(true);
  });

  it('baseName returns the last path segment', () => {
    expect(baseName('Show/Season 1/ep01.mkv')).toBe('ep01.mkv');
    expect(baseName('movie.mp4')).toBe('movie.mp4');
  });
});
