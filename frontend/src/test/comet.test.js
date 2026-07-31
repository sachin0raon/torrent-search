import { describe, it, expect } from 'vitest';
import { streamsToItems, parseStreamTitle } from '../api/comet.js';

describe('parseStreamTitle (Comet — description field, 🔎 provider)', () => {
  it('parses title/seeders/size/provider from description', () => {
    const parsed = parseStreamTitle('📄 Movie.2026.2160p.mkv\n👤 42 💾 62.9 GB 🔎 Knaben');
    expect(parsed.title).toBe('Movie.2026.2160p.mkv');
    expect(parsed.seeders).toBe(42);
    expect(parsed.size).toBe('62.9 GB');
    expect(parsed.source).toBe('Knaben');
  });

  it('handles missing metadata (debrid-cached entries)', () => {
    const parsed = parseStreamTitle('📄 Just A Title');
    expect(parsed.title).toBe('Just A Title');
    expect(parsed.seeders).toBeNull();
    expect(parsed.size).toBeNull();
    expect(parsed.source).toBeNull();
  });
});

describe('streamsToItems (Comet)', () => {
  it('builds magnets and skips entries with no infoHash', () => {
    const streams = [
      {
        description: '📄 Movie A\n👤 42 💾 1.34 GB 🔎 Knaben',
        infoHash: '633894b8378e4837dc551394c0637a35eb909c99',
      },
      { description: 'No hash here', behaviorHints: {} },
    ];
    const items = streamsToItems(streams);
    expect(items).toHaveLength(1);
    expect(items[0].seeders).toBe(42);
    expect(items[0].magnet).toMatch(/^magnet:\?dn=/);
  });

  it('dedups entries that produce an identical magnet', () => {
    const streams = [
      { description: '📄 Movie\n👤 100 💾 1 GB 🔎 ProviderA', infoHash: '633894b8378e4837dc551394c0637a35eb909c99' },
      { description: '📄 Movie\n👤 50 💾 1 GB 🔎 ProviderB', infoHash: '633894b8378e4837dc551394c0637a35eb909c99' },
      { description: '📄 Movie\n👤 10 💾 1 GB 🔎 ProviderC', infoHash: '4f3090b93b1520c36f7c13bc77dcb73bb5121685' },
    ];
    const items = streamsToItems(streams);
    expect(items).toHaveLength(2);
    expect(new Set(items.map((i) => i.magnet)).size).toBe(2);
  });

  it('returns an empty list for no streams', () => {
    expect(streamsToItems([])).toEqual([]);
    expect(streamsToItems(undefined)).toEqual([]);
  });

  it('sorts by seeders descending, with unknown (null) seeders last', () => {
    const streams = [
      { description: '📄 Low\n👤 5 💾 1 GB', infoHash: '1111111111111111111111111111111111111111' },
      { description: '📄 Unknown (debrid-cached)', infoHash: '2222222222222222222222222222222222222222' },
      { description: '📄 High\n👤 42 💾 1 GB', infoHash: '3333333333333333333333333333333333333333' },
    ];
    const items = streamsToItems(streams);
    expect(items.map((i) => i.seeders)).toEqual([42, 5, null]);
  });
});
