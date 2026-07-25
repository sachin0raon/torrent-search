import { useState } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import DiscoverSection from '../components/DiscoverSection.jsx';
import { api } from '../api/client.js';

vi.mock('../api/client.js', () => ({
  api: { discover: vi.fn() },
}));

const ALL_LABELS = [
  'Trending Movies',
  'Trending TV',
  'Popular Movies',
  'Popular TV',
  'All-Time Favorite Movies',
  'All-Time Favorite TV',
];

// Distinct title per category+mediaType so simultaneously-mounted (but hidden)
// panels never collide on the same text in a query.
function itemFor(category, mediaType) {
  return { tmdb_id: 1, media_type: mediaType, title: `${category}-${mediaType}-item` };
}

// DiscoverSection is a controlled component (active/onToggleBadge come from the
// parent); this harness plays App.jsx's role so the interaction tests below
// exercise the real click -> toggle -> re-render loop.
function Harness({ onSelect, initialActive = null }) {
  const [active, setActive] = useState(initialActive);
  function onToggleBadge(key) {
    setActive((prev) => (prev === key ? null : key));
  }
  return <DiscoverSection onSelect={onSelect} active={active} onToggleBadge={onToggleBadge} />;
}

describe('DiscoverSection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.discover.mockImplementation(({ category, mediaType }) =>
      Promise.resolve({ results: [itemFor(category, mediaType)] }),
    );
  });

  it('renders all 6 badges but no list and no fetch when nothing is active', () => {
    render(<Harness onSelect={() => {}} />);
    for (const label of ALL_LABELS) {
      expect(screen.getByRole('tab', { name: label })).toBeInTheDocument();
    }
    expect(screen.queryByText('trending-movie-item')).not.toBeInTheDocument();
    expect(api.discover).not.toHaveBeenCalled();
  });

  it('clicking a badge shows only that list and fetches only it', async () => {
    render(<Harness onSelect={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));

    await waitFor(() => expect(screen.getByText('trending-movie-item')).toBeInTheDocument());
    expect(api.discover).toHaveBeenCalledTimes(1);
    expect(api.discover).toHaveBeenCalledWith({ category: 'trending', mediaType: 'movie', page: 1 });
    expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'true');
  });

  it('clicking the active badge again deselects it and hides its panel', async () => {
    const { container } = render(<Harness onSelect={() => {}} />);
    const badge = screen.getByRole('tab', { name: 'Popular TV' });

    await userEvent.click(badge);
    await waitFor(() => expect(api.discover).toHaveBeenCalledTimes(1));
    expect(container.querySelector('.discover-panel')).not.toHaveClass('discover-panel-hidden');

    await userEvent.click(badge);
    expect(container.querySelector('.discover-panel')).toHaveClass('discover-panel-hidden');
    expect(badge).toHaveAttribute('aria-selected', 'false');
  });

  it('switching badges activates only the newly clicked one', async () => {
    render(<Harness onSelect={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(api.discover).toHaveBeenCalledTimes(1));

    await userEvent.click(screen.getByRole('tab', { name: 'Popular TV' }));
    await waitFor(() => expect(api.discover).toHaveBeenCalledTimes(2));
    expect(api.discover).toHaveBeenLastCalledWith({ category: 'popular', mediaType: 'tv', page: 1 });
    expect(screen.getByRole('tab', { name: 'Trending Movies' })).toHaveAttribute('aria-selected', 'false');
    expect(screen.getByRole('tab', { name: 'Popular TV' })).toHaveAttribute('aria-selected', 'true');
  });

  it('switching back to a previously-viewed badge does not re-fetch', async () => {
    render(<Harness onSelect={() => {}} />);
    const trending = screen.getByRole('tab', { name: 'Trending Movies' });
    const popular = screen.getByRole('tab', { name: 'Popular TV' });

    await userEvent.click(trending);
    await waitFor(() => expect(api.discover).toHaveBeenCalledTimes(1));
    await userEvent.click(popular);
    await waitFor(() => expect(api.discover).toHaveBeenCalledTimes(2));

    await userEvent.click(trending);
    expect(trending).toHaveAttribute('aria-selected', 'true');
    expect(api.discover).toHaveBeenCalledTimes(2); // no new fetch
  });

  it('parent clearing `active` hides the panel without unmounting it (no re-fetch on re-activation)', async () => {
    function ParentControlled() {
      const [active, setActive] = useState(null);
      return (
        <>
          <button onClick={() => setActive(null)}>external clear</button>
          <DiscoverSection
            onSelect={() => {}}
            active={active}
            onToggleBadge={(key) => setActive((prev) => (prev === key ? null : key))}
          />
        </>
      );
    }
    const { container } = render(<ParentControlled />);
    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    await waitFor(() => expect(api.discover).toHaveBeenCalledTimes(1));

    // Simulate App.jsx clearing `active` on title selection (not via a badge click).
    await userEvent.click(screen.getByText('external clear'));
    expect(container.querySelector('.discover-panel')).toHaveClass('discover-panel-hidden');

    await userEvent.click(screen.getByRole('tab', { name: 'Trending Movies' }));
    expect(screen.getByText('trending-movie-item')).toBeInTheDocument();
    expect(api.discover).toHaveBeenCalledTimes(1); // still cached, no re-fetch
  });

  it('respects an initial active badge (e.g. restored from persisted state)', async () => {
    render(<Harness onSelect={() => {}} initialActive="trending-tv" />);
    expect(screen.getByRole('tab', { name: 'Trending TV' })).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => expect(screen.getByText('trending-tv-item')).toBeInTheDocument());
  });
});
