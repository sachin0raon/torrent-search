import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import DownloadsModal from '../components/DownloadsModal.jsx';
import { downloader } from '../api/downloader.js';

vi.mock('../api/downloader.js', () => ({
  downloader: {
    listDownloads: vi.fn(),
    getDownload: vi.fn(),
    deleteDownload: vi.fn(),
    getDiskSpace: vi.fn().mockResolvedValue({ totalBytes: 100000000, freeBytes: 50000000, usedBytes: 50000000 }),
  },
}));

describe('DownloadsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue() } });
  });

  it('shows an empty state when there are no downloads', async () => {
    downloader.listDownloads.mockResolvedValue([]);
    render(<DownloadsModal onClose={vi.fn()} />);
    expect(await screen.findByText('No downloads yet.')).toBeInTheDocument();
  });

  it('lists downloads with progress and expands to show selected files', async () => {
    downloader.listDownloads.mockResolvedValue([
      { hash: 'aaaa', name: 'Movie', state: 'downloading', progress: 0.5, dlspeed: 1024, size: 2000 },
    ]);
    downloader.getDownload.mockResolvedValue({
      hash: 'aaaa',
      name: 'Movie',
      files: [
        { index: 0, name: 'movie.mkv', size: 2000, downloaded: 1000, selected: true },
        { index: 1, name: 'sample.mkv', size: 100, downloaded: 0, selected: false },
      ],
    });

    render(<DownloadsModal onClose={vi.fn()} />);

    expect(await screen.findByText('Movie')).toBeInTheDocument();
    expect(screen.getByText('downloading')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /show files/i }));

    // Only the selected file is shown.
    expect(await screen.findByText('movie.mkv')).toBeInTheDocument();
    expect(screen.queryByText('sample.mkv')).not.toBeInTheDocument();

    // Player deep links + plain download link are present.
    expect(screen.getByRole('button', { name: 'VLC' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /download file/i })).toBeInTheDocument();
  });

  it('deletes a download after a confirm click', async () => {
    downloader.listDownloads.mockResolvedValue([
      { hash: 'aaaa', name: 'Movie', state: 'pausedUP', progress: 1, dlspeed: 0, size: 2000 },
    ]);
    downloader.deleteDownload.mockResolvedValue(undefined);

    render(<DownloadsModal onClose={vi.fn()} />);
    expect(await screen.findByText('Movie')).toBeInTheDocument();

    const deleteButton = screen.getByRole('button', { name: /^delete$/i });
    await userEvent.click(deleteButton);
    // First click just asks for confirmation — no API call yet.
    expect(downloader.deleteDownload).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: /confirm delete/i })).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /confirm delete/i }));
    await waitFor(() => expect(downloader.deleteDownload).toHaveBeenCalledWith('aaaa'));
    await waitFor(() => expect(screen.queryByText('Movie')).not.toBeInTheDocument());
  });

  // Regression: file detail used to be fetched once on first expand and
  // cached forever, so a season pack's individual file progress froze at
  // whatever it was when the card was first opened, even though the parent
  // card's aggregate progress kept updating. Re-fetching on every expand
  // (a stand-in here for the live poll-while-expanded behavior — see
  // DownloadsModal.jsx's per-card useEffect) is what makes it not stuck.
  it('refreshes per-file progress on every expand rather than caching it forever', async () => {
    downloader.listDownloads.mockResolvedValue([
      { hash: 'aaaa', name: 'Season Pack', state: 'downloading', progress: 0.4, dlspeed: 1024, size: 2000 },
    ]);
    downloader.getDownload
      .mockResolvedValueOnce({
        hash: 'aaaa',
        name: 'Season Pack',
        files: [{ index: 0, name: 'e01.mkv', size: 1000, downloaded: 200, selected: true }],
      })
      .mockResolvedValueOnce({
        hash: 'aaaa',
        name: 'Season Pack',
        files: [{ index: 0, name: 'e01.mkv', size: 1000, downloaded: 800, selected: true }],
      });

    render(<DownloadsModal onClose={vi.fn()} />);
    expect(await screen.findByText('Season Pack')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /show files/i }));
    expect(await screen.findByText(/20%/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /hide files/i }));
    await userEvent.click(screen.getByRole('button', { name: /show files/i }));

    expect(await screen.findByText(/80%/)).toBeInTheDocument();
    expect(downloader.getDownload).toHaveBeenCalledTimes(2);
  });

  it('shows the "N of M files" indicator on the parent card for a season pack, and downloaded/total size', async () => {
    downloader.listDownloads.mockResolvedValue([
      {
        hash: 'aaaa', name: 'Show.S01', state: 'downloading', progress: 0.4, dlspeed: 1024,
        size: 2000, downloaded: 800, eta: 125, selectedFiles: 2, totalFiles: 10,
      },
    ]);

    render(<DownloadsModal onClose={vi.fn()} />);

    expect(await screen.findByText('Show.S01')).toBeInTheDocument();
    expect(screen.getByText(/2 of 10 files/)).toBeInTheDocument();
    // Each value is its own nowrap span (see .meta-chunk in styles.css), so
    // "800 B / 2.0 KB" is three separate text nodes, not one string.
    expect(screen.getByText('800 B')).toBeInTheDocument();
    expect(screen.getByText('2.0 KB')).toBeInTheDocument();
    expect(screen.getByText(/ETA 2m/)).toBeInTheDocument();
  });

  it('hides the "N of M files" indicator for a single-file download', async () => {
    downloader.listDownloads.mockResolvedValue([
      {
        hash: 'aaaa', name: 'Movie', state: 'downloading', progress: 0.5, dlspeed: 1024,
        size: 2000, downloaded: 1000, selectedFiles: 1, totalFiles: 1,
      },
    ]);

    render(<DownloadsModal onClose={vi.fn()} />);

    expect(await screen.findByText('Movie')).toBeInTheDocument();
    expect(screen.queryByText(/of 1 files/)).not.toBeInTheDocument();
  });

  // Regression guard for the fix in this change: the parent card used to
  // read its progress/state purely from the list poll's `entry` prop, which
  // could visibly disagree with the file rows fetched by the separate
  // expanded-card poll (different 5s timers, not phase-aligned). Once
  // expanded, the parent card's own numbers should track the same detail
  // response the file rows come from.
  it('updates the parent card\'s own progress from the detail response once expanded', async () => {
    downloader.listDownloads.mockResolvedValue([
      { hash: 'aaaa', name: 'Movie', state: 'downloading', progress: 0.4, dlspeed: 1024, size: 2000, downloaded: 800 },
    ]);
    downloader.getDownload.mockResolvedValue({
      hash: 'aaaa', name: 'Movie', state: 'downloading', progress: 0.9, dlspeed: 2048, size: 2000, downloaded: 1800,
      files: [{ index: 0, name: 'movie.mkv', size: 2000, downloaded: 1800, selected: true }],
    });

    render(<DownloadsModal onClose={vi.fn()} />);
    expect(await screen.findByText('Movie')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /show files/i }));

    // 90% now appears twice (parent card + the single file row) — assert via
    // findAllByText rather than requiring a single match.
    await waitFor(async () => expect((await screen.findAllByText(/90%/)).length).toBeGreaterThanOrEqual(1));
  });

  it('shows an error state when the download manager is unreachable', async () => {
    downloader.listDownloads.mockRejectedValue(new Error('qbittorrent unavailable'));
    render(<DownloadsModal onClose={vi.fn()} />);
    expect(await screen.findByText(/couldn't reach the download manager/i)).toBeInTheDocument();
  });
});
