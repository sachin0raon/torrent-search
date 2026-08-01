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

  it('shows an error state when the download manager is unreachable', async () => {
    downloader.listDownloads.mockRejectedValue(new Error('qbittorrent unavailable'));
    render(<DownloadsModal onClose={vi.fn()} />);
    expect(await screen.findByText(/couldn't reach the download manager/i)).toBeInTheDocument();
  });
});
