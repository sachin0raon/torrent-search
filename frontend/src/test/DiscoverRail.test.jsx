import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import DiscoverRail from '../components/DiscoverRail.jsx';
import { api } from '../api/client.js';

vi.mock('../api/client.js', () => ({
  api: { discover: vi.fn() },
}));

function titles(n, offset = 0) {
  return Array.from({ length: n }, (_, i) => ({
    tmdb_id: offset + i + 1,
    media_type: 'movie',
    title: `Title ${offset + i + 1}`,
    year: '2024',
    poster_url: null,
    overview: null,
  }));
}

describe('DiscoverRail', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('fetches page 1 on mount and renders items', async () => {
    api.discover.mockResolvedValue({ results: titles(10) });
    render(<DiscoverRail category="trending" mediaType="movie" label="Trending Movies" onSelect={() => {}} />);

    await waitFor(() => expect(screen.getByText('Title 1')).toBeInTheDocument());
    expect(api.discover).toHaveBeenCalledWith({ category: 'trending', mediaType: 'movie', page: 1 });
    expect(screen.getByText('Load more')).toBeInTheDocument();
  });

  it('hides Load more when fewer than a full page comes back', async () => {
    api.discover.mockResolvedValue({ results: titles(3) });
    render(<DiscoverRail category="trending" mediaType="movie" label="Trending Movies" onSelect={() => {}} />);

    await waitFor(() => expect(screen.getByText('Title 1')).toBeInTheDocument());
    expect(screen.queryByText('Load more')).not.toBeInTheDocument();
  });

  it('appends the next page on Load more', async () => {
    api.discover
      .mockResolvedValueOnce({ results: titles(10, 0) })
      .mockResolvedValueOnce({ results: titles(5, 10) });
    render(<DiscoverRail category="trending" mediaType="movie" label="Trending Movies" onSelect={() => {}} />);

    await waitFor(() => expect(screen.getByText('Title 1')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Load more'));

    await waitFor(() => expect(screen.getByText('Title 11')).toBeInTheDocument());
    expect(api.discover).toHaveBeenLastCalledWith({ category: 'trending', mediaType: 'movie', page: 2 });
    expect(screen.queryByText('Load more')).not.toBeInTheDocument();
  });

  it('shows a scoped error with Retry on failure', async () => {
    api.discover.mockRejectedValueOnce(new Error('Discover failed: 502'));
    render(<DiscoverRail category="popular" mediaType="tv" label="Popular TV" onSelect={() => {}} />);

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Discover failed: 502'));

    api.discover.mockResolvedValueOnce({ results: titles(2) });
    await userEvent.click(screen.getByText('Retry'));
    await waitFor(() => expect(screen.getByText('Title 1')).toBeInTheDocument());
  });

  it('calls onSelect with the clicked title', async () => {
    api.discover.mockResolvedValue({ results: titles(1) });
    const onSelect = vi.fn();
    render(<DiscoverRail category="trending" mediaType="movie" label="Trending Movies" onSelect={onSelect} />);

    await waitFor(() => expect(screen.getByText('Title 1')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Title 1'));
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ tmdb_id: 1 }));
  });
});
