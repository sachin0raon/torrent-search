import { describe, it, expect } from 'vitest';
import { streamsToItems, parseStreamTitle } from '../api/meteor.js';

describe('parseStreamTitle (Meteor — description field, 📺 quality -> source)', () => {
  it('parses title/size/quality from description', () => {
    const parsed = parseStreamTitle('📄 Movie.2026.1080p.WEB.h264-ETHEL\n📺 1080p | webdl | h264\n💾 5.45 GiB');
    expect(parsed.title).toBe('Movie.2026.1080p.WEB.h264-ETHEL');
    expect(parsed.size).toBe('5.45 GiB');
    expect(parsed.source).toBe('1080p | webdl | h264');
    expect(parsed.seeders).toBeNull();
  });

  it('opportunistically matches seeders when present', () => {
    const parsed = parseStreamTitle('📄 Title\n👤 12,345 💾 2 GB 📺 720p');
    expect(parsed.seeders).toBe(12345);
  });

  it('handles missing metadata', () => {
    const parsed = parseStreamTitle('📄 Just A Title');
    expect(parsed.title).toBe('Just A Title');
    expect(parsed.seeders).toBeNull();
    expect(parsed.size).toBeNull();
    expect(parsed.source).toBeNull();
  });
});

describe('streamsToItems (Meteor)', () => {
  it('builds magnets and skips entries with no infoHash', () => {
    const streams = [
      {
        description: '📄 Movie A\n📺 720p | webrip\n💾 969 MiB',
        infoHash: 'bac40d5cd3a67bb3659b14ad694776f8f0341d09',
        sources: ['tracker:udp://tracker.opentrackr.org:1337/announce'],
      },
      { description: 'No hash here' },
    ];
    const items = streamsToItems(streams);
    expect(items).toHaveLength(1);
    expect(items[0].magnet).toMatch(/^magnet:\?dn=/);
    expect(items[0].magnet).toContain('&tr=');
  });

  it('dedups entries that produce an identical magnet', () => {
    const streams = [
      { description: '📄 Movie\n📺 720p\n💾 1 GB', infoHash: '633894b8378e4837dc551394c0637a35eb909c99' },
      { description: '📄 Movie\n📺 720p\n💾 1 GB', infoHash: '633894b8378e4837dc551394c0637a35eb909c99' },
      { description: '📄 Movie\n📺 1080p\n💾 2 GB', infoHash: '4f3090b93b1520c36f7c13bc77dcb73bb5121685' },
    ];
    const items = streamsToItems(streams);
    expect(items).toHaveLength(2);
    expect(new Set(items.map((i) => i.magnet)).size).toBe(2);
  });

  it('returns an empty list for no streams', () => {
    expect(streamsToItems([])).toEqual([]);
    expect(streamsToItems(undefined)).toEqual([]);
  });

  it('preserves upstream order (no local sort)', () => {
    const streams = [
      { description: '📄 First\n📺 720p\n💾 1 GB', infoHash: 'aaaa000000000000000000000000000000000a' },
      { description: '📄 Second\n📺 1080p\n💾 2 GB', infoHash: 'bbbb000000000000000000000000000000000b' },
    ];
    const items = streamsToItems(streams);
    expect(items.map((i) => i.title)).toEqual(['First', 'Second']);
  });
});
