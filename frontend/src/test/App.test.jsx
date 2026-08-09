import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from '../App.jsx';
import { SessionProvider } from '../sessionContext.jsx';
import { api } from '../api/client.js';
import { setTorrentioMode } from '../torrentioMode.js';
import { setCometMode } from '../cometMode.js';
import { setMeteorMode } from '../meteorMode.js';

// App uses useSessions() (for the stats FAB), which requires a SessionProvider
// ancestor — main.jsx supplies one in production; tests must too.
function renderApp() {
  return render(
    <SessionProvider>
      <App />
    </SessionProvider>,
  );
}

vi.mock('../api/client.js', () => ({
  api: {
    discover: vi.fn(),
    searchTitles: vi.fn(),
    externalIds: vi.fn(),
    tvSeasons: vi.fn(),
    torrentio: vi.fn(),
    comet: vi.fn(),
    meteor: vi.fn(),
    forumSearch: vi.fn(),
    forumTopic: vi.fn(),
    x1337Search: vi.fn(),
    x1337Magnet: vi.fn(),
    getConfig: vi.fn(),
    setConfig: vi.fn(),
  },
}));

const movie = { tmdb_id: 5, media_type: 'movie', title: 'Test Movie', year: null, poster_url: null, overview: null };

const EMPTY_RESULT = { ok: true, error: null, items: [] };

describe('App — Discover-to-streams flow', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    // Force all three torrent sources server-side so the parallel client-side
    // fetch modules (which hit real network) are never invoked in tests. Go
    // through the real setters (not a raw localStorage.setItem) so each
    // module's in-memory cache stays in sync with what we just set.
    setTorrentioMode('server');
    setCometMode('server');
    setMeteorMode('server');
    api.getConfig.mockResolvedValue({});
    api.discover.mockResolvedValue({ results: [movie] });
    api.externalIds.mockResolvedValue({ imdb_id: 'tt123' });
    api.torrentio.mockResolvedValue({ ...EMPTY_RESULT });
    api.comet.mockResolvedValue({ ...EMPTY_RESULT });
    api.meteor.mockResolvedValue({ ...EMPTY_RESULT });
    api.forumSearch.mockResolvedValue({ ...EMPTY_RESULT });
    api.x1337Search.mockResolvedValue({ ...EMPTY_RESULT });
    api.x1337Magnet.mockResolvedValue({ magnet: 'magnet:?xt=test' });
  });

  it('picking a movie straight from Discover sends the title\'s own name/imdb to every source', async () => {
    renderApp();

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());

    await userEvent.click(screen.getByText('Test Movie'));

    await waitFor(() => expect(api.torrentio).toHaveBeenCalled());
    await waitFor(() => expect(api.forumSearch).toHaveBeenCalled());

    const torrentioCall = api.torrentio.mock.calls[0][0];
    expect(torrentioCall.imdbId).toBe('tt123');
    expect(torrentioCall.mediaType).toBe('movie');
    expect(api.comet.mock.calls[0][0].imdbId).toBe('tt123');
    expect(api.meteor.mock.calls[0][0].imdbId).toBe('tt123');
    expect(api.forumSearch.mock.calls[0][0].rawQuery).toBe('Test Movie');
  });

  it('picking a Discover title after an earlier search does not reuse the stale search query', async () => {
    api.searchTitles.mockResolvedValue({ results: [] });
    renderApp();

    await userEvent.type(screen.getByLabelText('Search query'), 'batman');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(api.searchTitles).toHaveBeenCalledWith('batman'));

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Test Movie'));

    await waitFor(() => expect(api.forumSearch).toHaveBeenCalled());
    expect(api.forumSearch.mock.calls[0][0].rawQuery).toBe('Test Movie'); // not 'batman'
  });

  it('each torrent source fetches and resolves independently (progressive per-tab)', async () => {
    let resolveComet;
    api.comet.mockReturnValue(new Promise((resolve) => { resolveComet = resolve; }));
    renderApp();

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Test Movie'));

    // Torrentio/Meteor/Forum resolve immediately; Comet is still pending.
    await waitFor(() => expect(screen.getByRole('tab', { name: /torrentio/i })).toBeInTheDocument());
    expect(screen.getByText(/fetching comet/i)).toBeInTheDocument();

    resolveComet({ ok: true, error: null, items: [] });
    await waitFor(() => expect(screen.queryByText(/fetching comet/i)).not.toBeInTheDocument());
  });

  it('retrying one source only re-fetches that source', async () => {
    api.comet.mockResolvedValueOnce({ ok: false, error: 'boom', items: [] });
    renderApp();

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Test Movie'));

    await waitFor(() => expect(api.comet).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(api.torrentio).toHaveBeenCalledTimes(1));

    // Default tab is Comet, which is now in an error state — click its Retry.
    await waitFor(() => expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: /retry/i }));

    await waitFor(() => expect(api.comet).toHaveBeenCalledTimes(2));
    expect(api.torrentio).toHaveBeenCalledTimes(1); // unaffected by Comet's retry
    expect(api.meteor).toHaveBeenCalledTimes(1);
    expect(api.forumSearch).toHaveBeenCalledTimes(1);
  });

  it('selecting a search result auto-hides an open Discover panel', async () => {
    api.searchTitles.mockResolvedValue({
      results: [{ tmdb_id: 9, media_type: 'movie', title: 'Searched Movie', year: null, poster_url: null, overview: null }],
    });
    const { container } = renderApp();

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
    const { container } = renderApp();

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());
    await userEvent.click(screen.getByText('Test Movie'));

    await waitFor(() =>
      expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'false'),
    );
    expect(container.querySelector('.discover-panel')).toHaveClass('discover-panel-hidden');
  });

  it('"← Change title" restores the Discover panel that was showing before selection', async () => {
    const { container } = renderApp();

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
    const { unmount } = renderApp();
    await userEvent.click(screen.getByRole('tab', { name: 'Trending TV' }));
    await waitFor(() => expect(api.discover).toHaveBeenCalled());
    unmount();

    renderApp();
    expect(screen.getByRole('tab', { name: 'Trending TV' })).toHaveAttribute('aria-selected', 'true');
  });

  it('auto-hiding Discover on item selection does not clear the persisted badge choice', async () => {
    const { unmount } = renderApp();
    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' })); // persists 'trending-movie'
    await waitFor(() => expect(screen.getByText('Test Movie')).toBeInTheDocument());

    await userEvent.click(screen.getByText('Test Movie')); // selects it -> hides in-memory only
    await waitFor(() => expect(api.forumSearch).toHaveBeenCalled());
    unmount();

    renderApp();
    // Still restored from localStorage even though the prior session's in-memory
    // state had cleared it on selection.
    expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'true');
  });
});
