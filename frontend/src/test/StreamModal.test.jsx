import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import StreamModal from '../components/StreamModal.jsx';
import { streamer } from '../api/streamer.js';

vi.mock('../api/streamer.js', () => ({
  streamer: { createSession: vi.fn(), getSession: vi.fn() },
}));

describe('StreamModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue() } });
  });

  it('lists files with player links and a copy stream URL button', async () => {
    streamer.createSession.mockResolvedValue({
      sessionId: 'sess',
      name: 'Movie',
      ready: true,
      files: [
        { index: 0, path: 'Movie/movie.mkv', size: 2147483648, streamable: true },
        { index: 1, path: 'Movie/readme.txt', size: 12, streamable: false },
      ],
    });

    render(<StreamModal magnet="magnet:?xt=1" title="Movie" onClose={() => {}} />);

    expect(await screen.findByText('movie.mkv')).toBeInTheDocument();
    // Player deep links present.
    expect(screen.getByRole('button', { name: 'VLC' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'nPlayer' })).toBeInTheDocument();
    // Copy stream URL button copies the absolute same-origin URL.
    await userEvent.click(screen.getByRole('button', { name: /copy stream url/i }));
    const copied = navigator.clipboard.writeText.mock.calls[0][0];
    expect(copied).toContain('/stream/sess/0/movie.mkv');
    // Non-streamable file marked as not playable.
    expect(screen.getByText('not playable')).toBeInTheDocument();
  });

  it('shows an error with retry when the service fails', async () => {
    streamer.createSession.mockRejectedValueOnce(new Error('couldn\'t fetch torrent info (no peers?)'));
    render(<StreamModal magnet="magnet:?xt=1" title="Movie" onClose={() => {}} />);

    expect(await screen.findByRole('alert')).toHaveTextContent('no peers');

    // Retry re-invokes the service.
    streamer.createSession.mockResolvedValueOnce({ sessionId: 's', name: 'M', ready: true, files: [] });
    await userEvent.click(screen.getByRole('button', { name: /retry/i }));
    await waitFor(() => expect(streamer.createSession).toHaveBeenCalledTimes(2));
  });

  it('notes when a torrent has no playable video files', async () => {
    streamer.createSession.mockResolvedValue({
      sessionId: 's',
      name: 'Pack',
      ready: true,
      files: [{ index: 0, path: 'notes.txt', size: 10, streamable: false }],
    });
    render(<StreamModal magnet="magnet:?xt=1" title="Pack" onClose={() => {}} />);
    expect(await screen.findByText('No playable video files in this torrent.')).toBeInTheDocument();
  });
});
