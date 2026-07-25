import { describe, it, expect } from 'vitest';
import { streamsToItems } from '../api/torrentio.js';

describe('streamsToItems', () => {
  it('builds magnets and skips entries with no infoHash', () => {
    const streams = [
      {
        title: 'Ubuntu 22.04\n👤 3945 💾 1.34 GB ⚙️ Bittorrent',
        infoHash: '633894b8378e4837dc551394c0637a35eb909c99',
      },
      { title: 'No hash here', behaviorHints: {} },
    ];
    const items = streamsToItems(streams);
    expect(items).toHaveLength(1);
    expect(items[0].seeders).toBe(3945);
    expect(items[0].magnet).toMatch(/^magnet:\?dn=/);
  });

  it('dedups entries that produce an identical magnet', () => {
    // Same torrent (title, infoHash, sources) listed twice, e.g. via two
    // providers — would otherwise produce two items with the same magnet,
    // which collide as a React key in ResultTabs.
    const streams = [
      { title: 'Movie\n👤 100 💾 1 GB ⚙️ ProviderA', infoHash: '633894b8378e4837dc551394c0637a35eb909c99' },
      { title: 'Movie\n👤 50 💾 1 GB ⚙️ ProviderB', infoHash: '633894b8378e4837dc551394c0637a35eb909c99' },
      { title: 'Movie\n👤 10 💾 1 GB ⚙️ ProviderC', infoHash: '4f3090b93b1520c36f7c13bc77dcb73bb5121685' },
    ];
    const items = streamsToItems(streams);
    expect(items).toHaveLength(2);
    expect(new Set(items.map((i) => i.magnet)).size).toBe(2);
    expect(items[0].seeders).toBe(100); // first-seen kept, not overwritten by the dup
  });

  it('returns an empty list for no streams', () => {
    expect(streamsToItems([])).toEqual([]);
    expect(streamsToItems(undefined)).toEqual([]);
  });
});
