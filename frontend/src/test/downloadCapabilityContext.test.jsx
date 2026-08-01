import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import ResultTabs from '../components/ResultTabs.jsx';
import { DownloadCapabilityProvider } from '../downloadCapabilityContext.jsx';
import { downloader } from '../api/downloader.js';

vi.mock('../api/downloader.js', () => ({
  downloader: { getStatus: vi.fn() },
}));

const sources = {
  comet: {
    ok: true,
    error: null,
    loading: false,
    items: [{ title: 'Movie', seeders: 1, size: '1 GB', source: 'BT', magnet: 'magnet:?xt=1' }],
  },
  meteor: { ok: true, error: null, items: [], loading: false },
  forum: { ok: true, error: null, items: [], loading: false },
  torrentio: { ok: true, error: null, items: [], loading: false },
};

describe('download capability gating', () => {
  beforeEach(() => vi.clearAllMocks());

  it('hides the Download button when the feature is unset (no provider)', () => {
    // No DownloadCapabilityProvider at all — the default context value (false)
    // must keep the button hidden rather than throwing.
    render(<ResultTabs sources={sources} />);
    expect(screen.queryByRole('button', { name: /^download$/i })).not.toBeInTheDocument();
  });

  it('hides the Download button while /download-api/status has not resolved true', () => {
    downloader.getStatus.mockResolvedValue({ enabled: false });
    render(
      <DownloadCapabilityProvider>
        <ResultTabs sources={sources} />
      </DownloadCapabilityProvider>,
    );
    expect(screen.queryByRole('button', { name: /^download$/i })).not.toBeInTheDocument();
  });

  it('shows the Download button once /download-api/status resolves enabled:true', async () => {
    downloader.getStatus.mockResolvedValue({ enabled: true });
    render(
      <DownloadCapabilityProvider>
        <ResultTabs sources={sources} />
      </DownloadCapabilityProvider>,
    );
    expect(await screen.findByRole('button', { name: /^download$/i })).toBeInTheDocument();
  });
});
