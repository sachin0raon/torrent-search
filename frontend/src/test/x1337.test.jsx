import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import X1337Row from '../components/X1337Row.jsx';
import { api } from '../api/client.js';

vi.mock('../api/client.js', () => ({
  api: {
    x1337Magnet: vi.fn(),
  },
}));

describe('X1337Row component', () => {
  const item = {
    title: 'Spider-Man No Way Home (2021)',
    detail_path: '/torrent/5178882/Spider-Man-No-Way-Home/',
    seeds: 500,
    leeches: 10,
    size: '2.5 GB',
    date: 'Mar 15',
  };

  beforeEach(() => {
    vi.clearAllMocks();
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue() },
    });
  });

  it('renders item stats and title', () => {
    render(<X1337Row item={item} />);
    expect(screen.getByText('Spider-Man No Way Home (2021)')).toBeInTheDocument();
    expect(screen.getByText('👤 500')).toBeInTheDocument();
    expect(screen.getByText('🔴 10')).toBeInTheDocument();
    expect(screen.getByText('💾 2.5 GB')).toBeInTheDocument();
  });

  it('lazy-fetches magnet when Copy Magnet is clicked', async () => {
    api.x1337Magnet.mockResolvedValueOnce({ magnet: 'magnet:?xt=urn:btih:hash1337' });
    render(<X1337Row item={item} />);

    const copyBtn = screen.getByRole('button', { name: /copy magnet/i });
    await userEvent.click(copyBtn);

    expect(api.x1337Magnet).toHaveBeenCalledWith({ path: '/torrent/5178882/Spider-Man-No-Way-Home/' });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('magnet:?xt=urn:btih:hash1337');
  });

  it('displays error banner if magnet fetch fails', async () => {
    api.x1337Magnet.mockRejectedValueOnce(new Error('Network error'));
    render(<X1337Row item={item} />);

    const copyBtn = screen.getByRole('button', { name: /copy magnet/i });
    await userEvent.click(copyBtn);

    expect(screen.getByRole('alert')).toHaveTextContent('Network error');
  });
});
