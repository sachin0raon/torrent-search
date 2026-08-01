import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import DownloadPanel from '../components/DownloadPanel.jsx';
import { downloader } from '../api/downloader.js';

vi.mock('../api/downloader.js', () => ({
  downloader: { createDownload: vi.fn(), selectFiles: vi.fn() },
}));

describe('DownloadPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('lists files as checkboxes and starts a download for the selected ones', async () => {
    downloader.createDownload.mockResolvedValue({
      hash: 'aaaa',
      name: 'Season Pack',
      files: [
        { index: 0, name: 'S01E01.mkv', size: 1000 },
        { index: 1, name: 'S01E02.mkv', size: 2000 },
      ],
    });
    downloader.selectFiles.mockResolvedValue(undefined);

    render(<DownloadPanel magnet="magnet:?xt=1" />);

    expect(await screen.findByText('S01E01.mkv')).toBeInTheDocument();
    expect(screen.getByText('S01E02.mkv')).toBeInTheDocument();

    // Nothing selected yet — the floating download button doesn't exist at all.
    expect(screen.queryByRole('button', { name: /download/i })).not.toBeInTheDocument();

    await userEvent.click(screen.getByText('S01E01.mkv'));
    const startButton = await screen.findByRole('button', { name: /download/i });
    expect(startButton).not.toBeDisabled();

    await userEvent.click(startButton);

    await waitFor(() => expect(downloader.selectFiles).toHaveBeenCalledWith('aaaa', [0]));
    expect(await screen.findByText(/check the downloads list/i)).toBeInTheDocument();
  });

  it('shows an error with retry when adding the torrent fails', async () => {
    downloader.createDownload.mockRejectedValueOnce(new Error('qbittorrent unavailable'));
    render(<DownloadPanel magnet="magnet:?xt=1" />);

    expect(await screen.findByRole('alert')).toHaveTextContent('qbittorrent unavailable');

    downloader.createDownload.mockResolvedValueOnce({ hash: 'a', name: 'M', files: [] });
    await userEvent.click(screen.getByRole('button', { name: /retry/i }));
    await waitFor(() => expect(downloader.createDownload).toHaveBeenCalledTimes(2));
  });

  it('notes when a torrent has no files', async () => {
    downloader.createDownload.mockResolvedValue({ hash: 'a', name: 'Empty', files: [] });
    render(<DownloadPanel magnet="magnet:?xt=1" />);
    expect(await screen.findByText('No files in this torrent.')).toBeInTheDocument();
  });

  it('a "Select all" toggle selects and deselects every file', async () => {
    downloader.createDownload.mockResolvedValue({
      hash: 'aaaa',
      name: 'Season Pack',
      files: [
        { index: 0, name: 'S01E01.mkv', size: 1000 },
        { index: 1, name: 'S01E02.mkv', size: 2000 },
        { index: 2, name: 'S01E03.mkv', size: 3000 },
      ],
    });
    render(<DownloadPanel magnet="magnet:?xt=1" />);
    expect(await screen.findByText('S01E01.mkv')).toBeInTheDocument();

    const selectAll = screen.getByRole('checkbox', { name: /select all/i });
    const fileCheckboxes = () => screen.getAllByRole('checkbox').filter((cb) => cb !== selectAll);

    await userEvent.click(selectAll);
    expect(fileCheckboxes().every((cb) => cb.checked)).toBe(true);
    expect(screen.getByRole('button', { name: /download 3 files/i })).toBeInTheDocument();

    await userEvent.click(screen.getByRole('checkbox', { name: /deselect all/i }));
    expect(fileCheckboxes().every((cb) => !cb.checked)).toBe(true);
  });

  it('does not show a "Select all" toggle for a single-file torrent', async () => {
    downloader.createDownload.mockResolvedValue({
      hash: 'aaaa',
      name: 'Movie',
      files: [{ index: 0, name: 'movie.mkv', size: 1000 }],
    });
    render(<DownloadPanel magnet="magnet:?xt=1" />);
    expect(await screen.findByText('movie.mkv')).toBeInTheDocument();
    expect(screen.queryByText(/select all/i)).not.toBeInTheDocument();
  });
});
