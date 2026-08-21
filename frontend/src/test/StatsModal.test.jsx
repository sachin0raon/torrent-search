import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import StatsModal from '../components/StatsModal.jsx';
import { SessionProvider } from '../sessionContext.jsx';
import { QbtActiveStreamsProvider } from '../qbtActiveStreamsContext.jsx';
import { DownloadCapabilityProvider } from '../downloadCapabilityContext.jsx';
import { streamer } from '../api/streamer.js';
import { downloader } from '../api/downloader.js';

vi.mock('../api/streamer.js', () => ({
  streamer: {
    listActiveTorrents: vi.fn(),
    resumeTorrent: vi.fn(),
    deleteTorrent: vi.fn(),
    moveToDownloads: vi.fn(),
    flushTorrents: vi.fn(),
    getStats: vi.fn(),
  },
}));

vi.mock('../api/downloader.js', () => ({
  downloader: { getStatus: vi.fn() },
}));

function renderModal({ qbtEnabled }) {
  streamer.listActiveTorrents.mockImplementation(() =>
    qbtEnabled ? Promise.resolve([]) : Promise.reject(Object.assign(new Error('not found'), { status: 404 })),
  );
  downloader.getStatus.mockResolvedValue({ enabled: false });
  return render(
    <SessionProvider>
      <DownloadCapabilityProvider>
        <QbtActiveStreamsProvider>
          <StatsModal onClose={() => {}} />
        </QbtActiveStreamsProvider>
      </DownloadCapabilityProvider>
    </SessionProvider>,
  );
}

describe('StatsModal', () => {
  beforeEach(() => vi.clearAllMocks());

  it('falls back to the local-session view when the engine does not support the panel (404)', async () => {
    renderModal({ qbtEnabled: false });
    // The capability probe 404s, so the qBittorrent-aware list is never
    // rendered — the plain "no active streams" (local-session) empty state
    // shows instead, same as today's behavior on anacrolix.
    expect(await screen.findByText(/no active streams/i)).toBeInTheDocument();
    expect(screen.queryByText(/flush all/i)).not.toBeInTheDocument();
  });

  it('renders the qBittorrent-aware list with a paused chip and actions', async () => {
    streamer.listActiveTorrents.mockResolvedValue([
      { hash: 'AAA', name: 'Movie.mkv', progress: 0.42, size: 1000, downloaded: 420, paused: true },
    ]);
    render(
      <SessionProvider>
        <DownloadCapabilityProvider>
          <QbtActiveStreamsProvider>
            <StatsModal onClose={() => {}} />
          </QbtActiveStreamsProvider>
        </DownloadCapabilityProvider>
      </SessionProvider>,
    );

    expect(await screen.findByText('Movie.mkv')).toBeInTheDocument();
    expect(screen.getByText('Paused')).toBeInTheDocument();
    expect(screen.getByText(/42%/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^resume$/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^delete$/i })).toBeInTheDocument();
    // Download Manager is off in this test (getStatus resolves enabled:false),
    // so Move to Downloads must not be offered.
    expect(screen.queryByRole('button', { name: /move to downloads/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /flush all/i })).toBeInTheDocument();
  });

  it('resume button calls the API and refreshes the list', async () => {
    streamer.listActiveTorrents.mockResolvedValue([
      { hash: 'AAA', name: 'Movie.mkv', progress: 1, size: 1000, downloaded: 1000, paused: true },
    ]);
    streamer.resumeTorrent.mockResolvedValue(undefined);
    render(
      <SessionProvider>
        <DownloadCapabilityProvider>
          <QbtActiveStreamsProvider>
            <StatsModal onClose={() => {}} />
          </QbtActiveStreamsProvider>
        </DownloadCapabilityProvider>
      </SessionProvider>,
    );

    const user = userEvent.setup();
    await screen.findByText('Movie.mkv');
    await user.click(screen.getByRole('button', { name: /^resume$/i }));

    await waitFor(() => expect(streamer.resumeTorrent).toHaveBeenCalledWith('AAA'));
    // 3 calls: QbtActiveStreamsProvider's own capability probe on mount, the
    // view's initial list fetch, and the post-action refresh.
    expect(streamer.listActiveTorrents).toHaveBeenCalledTimes(3);
  });

  it('flush button calls the API and clears the list', async () => {
    let torrents = [{ hash: 'AAA', name: 'Movie.mkv', progress: 1, size: 1000, downloaded: 1000, paused: false }];
    streamer.listActiveTorrents.mockImplementation(() => Promise.resolve(torrents));
    streamer.flushTorrents.mockImplementation(() => {
      torrents = [];
      return Promise.resolve(undefined);
    });
    render(
      <SessionProvider>
        <DownloadCapabilityProvider>
          <QbtActiveStreamsProvider>
            <StatsModal onClose={() => {}} />
          </QbtActiveStreamsProvider>
        </DownloadCapabilityProvider>
      </SessionProvider>,
    );

    const user = userEvent.setup();
    await screen.findByText('Movie.mkv');
    await user.click(screen.getByRole('button', { name: /flush all/i }));

    await waitFor(() => expect(streamer.flushTorrents).toHaveBeenCalled());
    expect(await screen.findByText(/no active streams/i)).toBeInTheDocument();
  });
});
