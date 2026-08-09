import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ResultTabs from '../components/ResultTabs.jsx';

const EMPTY = { ok: true, error: null, items: [], loading: false };

const baseSources = {
  comet: {
    ok: true,
    error: null,
    loading: false,
    items: [
      { title: 'Movie 1080p', seeders: 123, size: '2 GB', source: 'BT', magnet: 'magnet:?xt=1' },
    ],
  },
  meteor: { ...EMPTY },
  forum: { ...EMPTY },
  x1337: { ...EMPTY },
  torrentio: { ...EMPTY },
};

describe('ResultTabs', () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue() },
    });
  });

  it('shows comet results (default tab) and copies magnet', async () => {
    render(<ResultTabs sources={baseSources} />);
    expect(screen.getByText('Movie 1080p')).toBeInTheDocument();
    expect(screen.getByText('👤 123')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /copy magnet/i }));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('magnet:?xt=1');
  });

  it('renders tabs in order Comet, Meteor, Forum, 1337x, Torrentio', () => {
    render(<ResultTabs sources={baseSources} />);
    const tabs = screen.getAllByRole('tab').map((t) => t.textContent);
    expect(tabs).toEqual(['Comet1', 'Meteor0', 'Forum0', '1337x0', 'Torrentio0']);
  });

  it('shows per-source error banner in forum tab', async () => {
    const sources = {
      ...baseSources,
      forum: { ok: false, error: 'Forum base URL is not configured', items: [], loading: false },
    };
    render(<ResultTabs sources={sources} />);
    await userEvent.click(screen.getByRole('tab', { name: /forum/i }));
    expect(screen.getByRole('alert')).toHaveTextContent('Forum base URL is not configured');
  });

  it('shows empty state when a source has no items', async () => {
    render(<ResultTabs sources={baseSources} />);
    await userEvent.click(screen.getByRole('tab', { name: /meteor/i }));
    expect(screen.getByText('No meteor results.')).toBeInTheDocument();
  });

  it('renders seeders fallback dash when seeders missing', () => {
    const sources = {
      ...baseSources,
      comet: {
        ok: true,
        error: null,
        loading: false,
        items: [{ title: 'X', seeders: null, size: null, source: null, magnet: 'magnet:?xt=2' }],
      },
    };
    render(<ResultTabs sources={sources} />);
    expect(screen.getByText('👤 —')).toBeInTheDocument();
  });

  it('shows a loading spinner for a source still fetching (no error yet)', async () => {
    const sources = {
      ...baseSources,
      torrentio: { ok: false, error: null, items: [], loading: true },
    };
    render(<ResultTabs sources={sources} />);
    await userEvent.click(screen.getByRole('tab', { name: /torrentio/i }));
    expect(screen.getByText(/fetching torrentio/i)).toBeInTheDocument();
  });

  it("Forum's Retry button renders and fires in the main (non-forumOnly) view", async () => {
    // Regression: previously ForumTab never received onRetry in the main tab
    // view, so its Retry button silently failed to render at all.
    const onRetryForum = vi.fn();
    const sources = {
      ...baseSources,
      forum: { ok: false, error: 'timed out', items: [], loading: false },
    };
    render(<ResultTabs sources={sources} onRetryForum={onRetryForum} />);
    await userEvent.click(screen.getByRole('tab', { name: /forum/i }));
    await userEvent.click(screen.getByRole('button', { name: /retry/i }));
    expect(onRetryForum).toHaveBeenCalledTimes(1);
  });

  it('each tab retries independently — clicking Retry on one tab does not call the others', async () => {
    const onRetryTorrentio = vi.fn();
    const onRetryComet = vi.fn();
    const onRetryMeteor = vi.fn();
    const onRetryForum = vi.fn();
    const sources = {
      comet: { ok: false, error: 'boom', items: [], loading: false },
      meteor: { ...EMPTY },
      forum: { ...EMPTY },
      torrentio: { ok: false, error: 'boom', items: [], loading: false },
    };
    render(
      <ResultTabs
        sources={sources}
        onRetryTorrentio={onRetryTorrentio}
        onRetryComet={onRetryComet}
        onRetryMeteor={onRetryMeteor}
        onRetryForum={onRetryForum}
      />,
    );
    // Default tab is Comet, which is in an error state — click its Retry.
    await userEvent.click(screen.getByRole('button', { name: /retry/i }));
    expect(onRetryComet).toHaveBeenCalledTimes(1);
    expect(onRetryTorrentio).not.toHaveBeenCalled();
    expect(onRetryMeteor).not.toHaveBeenCalled();
    expect(onRetryForum).not.toHaveBeenCalled();
  });

  it('an error banner has a working dismiss button that hides just the message', async () => {
    const sources = {
      ...baseSources,
      torrentio: { ok: false, error: 'timed out', items: [], loading: false },
    };
    render(<ResultTabs sources={sources} onRetryTorrentio={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: /torrentio/i }));

    const banner = screen.getByRole('alert');
    expect(banner).toHaveTextContent('Torrentio: timed out');
    await userEvent.click(screen.getByRole('button', { name: /dismiss/i }));

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    // Retry stays available even after the banner is dismissed.
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
  });

  it('a fresh error (e.g. after a failed retry) re-appears even if the prior one was dismissed', async () => {
    const sources = {
      ...baseSources,
      torrentio: { ok: false, error: 'timed out', items: [], loading: false },
    };
    const { rerender } = render(<ResultTabs sources={sources} onRetryTorrentio={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: /torrentio/i }));
    await userEvent.click(screen.getByRole('button', { name: /dismiss/i }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    // Retry starts: the loading spinner replaces the error entirely.
    rerender(
      <ResultTabs
        sources={{ ...sources, torrentio: { ok: false, error: null, items: [], loading: true } }}
        onRetryTorrentio={vi.fn()}
      />,
    );
    expect(screen.getByText(/fetching torrentio/i)).toBeInTheDocument();

    // Retry fails again with the same message — it should be visible again,
    // not still hidden from the earlier dismissal.
    rerender(
      <ResultTabs
        sources={{ ...sources, torrentio: { ok: false, error: 'timed out', items: [], loading: false } }}
        onRetryTorrentio={vi.fn()}
      />,
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Torrentio: timed out');
  });

  it('forumOnly renders Forum and 1337x tabs and wires retries', async () => {
    const onRetryForum = vi.fn();
    const sources = { forum: { ok: false, error: 'timed out', items: [], loading: false } };
    render(<ResultTabs sources={sources} forumOnly onRetryForum={onRetryForum} />);
    expect(screen.getAllByRole('tab')).toHaveLength(2);
    await userEvent.click(screen.getByRole('button', { name: /retry/i }));
    expect(onRetryForum).toHaveBeenCalledTimes(1);
  });
});
