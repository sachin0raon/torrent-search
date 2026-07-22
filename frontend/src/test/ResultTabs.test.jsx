import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ResultTabs from '../components/ResultTabs.jsx';

const baseStreams = {
  torrentio: {
    ok: true,
    error: null,
    items: [
      { title: 'Movie 1080p', seeders: 123, size: '2 GB', source: 'BT', magnet: 'magnet:?xt=1' },
    ],
  },
  forum: { ok: true, error: null, items: [] },
};

describe('ResultTabs', () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue() },
    });
  });

  it('shows torrentio results and copies magnet', async () => {
    render(<ResultTabs streams={baseStreams} />);
    expect(screen.getByText('Movie 1080p')).toBeInTheDocument();
    expect(screen.getByText('👤 123')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /copy magnet/i }));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('magnet:?xt=1');
  });

  it('shows per-source error banner in forum tab', async () => {
    const streams = {
      ...baseStreams,
      forum: { ok: false, error: 'Forum base URL is not configured', items: [] },
    };
    render(<ResultTabs streams={streams} />);
    await userEvent.click(screen.getByRole('tab', { name: /forum/i }));
    expect(screen.getByRole('alert')).toHaveTextContent('Forum base URL is not configured');
  });

  it('shows empty state when a source has no items', async () => {
    render(<ResultTabs streams={baseStreams} />);
    await userEvent.click(screen.getByRole('tab', { name: /forum/i }));
    expect(screen.getByText('No forum results.')).toBeInTheDocument();
  });

  it('renders seeders fallback dash when seeders missing', () => {
    const streams = {
      ...baseStreams,
      torrentio: {
        ok: true,
        error: null,
        items: [{ title: 'X', seeders: null, size: null, source: null, magnet: 'magnet:?xt=2' }],
      },
    };
    render(<ResultTabs streams={streams} />);
    expect(screen.getByText('👤 —')).toBeInTheDocument();
  });
});
