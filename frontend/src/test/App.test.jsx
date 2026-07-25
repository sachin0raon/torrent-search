import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from '../App.jsx';
import { api } from '../api/client.js';

vi.mock('../api/client.js', () => ({
  api: {
    discover: vi.fn(),
    searchTitles: vi.fn(),
    externalIds: vi.fn(),
    tvSeasons: vi.fn(),
    streams: vi.fn(),
    forumTopic: vi.fn(),
    getConfig: vi.fn(),
    setConfig: vi.fn(),
  },
}));

const movie = { tmdb_id: 5, media_type: 'movie', title: 'Test Movie', year: null, poster_url: null, overview: null };

describe('App — Discover-to-streams flow', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    localStorage.setItem('torrentioMode', 'server'); // skip the parallel client-side Torrentio fetch
    api.discover.mockResolvedValue({ results: [movie] });
    api.externalIds.mockResolvedValue({ imdb_id: 'tt123' });
    api.streams.mockResolvedValue({
      torrentio: { ok: true, error: null, items: [] },
      forum: { ok: true, error: null, items: [] },
    });
  });

  it('picking a movie straight from Discover sends a non-empty raw_query', async () => {
    render(<App />);

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());

    await userEvent.click(screen.getByText('Test Movie'));

    await waitFor(() => expect(api.streams).toHaveBeenCalled());
    const call = api.streams.mock.calls[0][0];
    expect(call.rawQuery).toBe('Test Movie');
    expect(call.imdbId).toBe('tt123');
    expect(call.mediaType).toBe('movie');
  });

  it('picking a Discover title after an earlier search does not reuse the stale search query', async () => {
    api.searchTitles.mockResolvedValue({ results: [] });
    render(<App />);

    await userEvent.type(screen.getByLabelText('Search query'), 'batman');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(api.searchTitles).toHaveBeenCalledWith('batman'));

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Test Movie'));

    await waitFor(() => expect(api.streams).toHaveBeenCalled());
    expect(api.streams.mock.calls[0][0].rawQuery).toBe('Test Movie'); // not 'batman'
  });

  it('selecting a search result auto-hides an open Discover panel', async () => {
    api.searchTitles.mockResolvedValue({
      results: [{ tmdb_id: 9, media_type: 'movie', title: 'Searched Movie', year: null, poster_url: null, overview: null }],
    });
    const { container } = render(<App />);

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText('Search query'), 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(screen.getByText('Searched Movie')).toBeInTheDocument());

    await userEvent.click(screen.getByText('Searched Movie'));

    await waitFor(() =>
      expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'false'),
    );
    expect(container.querySelector('.discover-panel')).toHaveClass('discover-panel-hidden');
  });

  it('selecting a Discover title also hides its own panel afterward', async () => {
    const { container } = render(<App />);

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Test Movie'));

    await waitFor(() =>
      expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'false'),
    );
    expect(container.querySelector('.discover-panel')).toHaveClass('discover-panel-hidden');
  });

  it('"← Change title" restores the Discover panel that was showing before selection', async () => {
    const { container } = render(<App />);

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());
    expect(api.discover).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByText('Test Movie'));
    await waitFor(() =>
      expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'false'),
    );

    await userEvent.click(screen.getByRole('button', { name: '← Change title' }));

    expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'true');
    const panel = container.querySelector('.discover-panel');
    expect(panel).not.toHaveClass('discover-panel-hidden');
    expect(within(panel).getByText('Test Movie')).toBeInTheDocument();
    expect(api.discover).toHaveBeenCalledTimes(1); // still cached, no re-fetch
  });

  it('persists the active Discover badge across remounts', async () => {
    const { unmount } = render(<App />);
    await userEvent.click(screen.getByRole('tab', { name: 'Trending TV' }));
    await waitFor(() => expect(api.discover).toHaveBeenCalled());
    unmount();

    render(<App />);
    expect(screen.getByRole('tab', { name: 'Trending TV' })).toHaveAttribute('aria-selected', 'true');
  });

  it('auto-hiding Discover on item selection does not clear the persisted badge choice', async () => {
    const { unmount } = render(<App />);
    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' })); // persists 'trending-movie'
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());

    await userEvent.click(screen.getByText('Test Movie')); // selects it -> hides in-memory only
    await waitFor(() => expect(api.streams).toHaveBeenCalled());
    unmount();

    render(<App />);
    // Still restored from localStorage even though the prior session's in-memory
    // state had cleared it on selection.
    expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'true');
  });
});
