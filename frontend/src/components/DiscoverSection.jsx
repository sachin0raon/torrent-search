import { useEffect, useState } from 'react';
import DiscoverRail from './DiscoverRail.jsx';

const RAILS = [
  { key: 'trending-movie', category: 'trending', mediaType: 'movie', label: 'Trending Movies' },
  { key: 'trending-tv', category: 'trending', mediaType: 'tv', label: 'Trending TV' },
  { key: 'popular-movie', category: 'popular', mediaType: 'movie', label: 'Popular Movies' },
  { key: 'popular-tv', category: 'popular', mediaType: 'tv', label: 'Popular TV' },
  { key: 'top_rated-movie', category: 'top_rated', mediaType: 'movie', label: 'All-Time Favorite Movies' },
  { key: 'top_rated-tv', category: 'top_rated', mediaType: 'tv', label: 'All-Time Favorite TV' },
];

// Discover: one badge per category+media_type; clicking a badge shows its list,
// clicking the active badge again hides it. Only one is visible at a time.
//
// `active` is controlled by the parent (App) rather than owned here, so App can
// clear it when the user picks any title (search or Discover) to auto-hide the
// panel. Once a badge has been activated its DiscoverRail stays mounted (just
// hidden) so switching back — including App clearing and a later re-click — is
// instant and re-uses its already-fetched items instead of re-fetching.
export default function DiscoverSection({ onSelect, active, onToggleBadge }) {
  const [everActive, setEverActive] = useState(() => (active ? new Set([active]) : new Set()));

  useEffect(() => {
    if (active && !everActive.has(active)) {
      setEverActive((prev) => new Set(prev).add(active));
    }
  }, [active, everActive]);

  return (
    <section className="discover-section">
      <div className="discover-badges" role="tablist">
        {RAILS.map((rail) => (
          <button
            key={rail.key}
            role="tab"
            className={`discover-badge${active === rail.key ? ' active' : ''}`}
            aria-selected={active === rail.key}
            onClick={() => onToggleBadge(rail.key)}
          >
            {rail.label}
          </button>
        ))}
      </div>

      {RAILS.filter((rail) => everActive.has(rail.key)).map((rail) => (
        <div
          key={rail.key}
          className={`discover-panel${active === rail.key ? '' : ' discover-panel-hidden'}`}
        >
          <DiscoverRail
            category={rail.category}
            mediaType={rail.mediaType}
            label={rail.label}
            onSelect={onSelect}
          />
        </div>
      ))}
    </section>
  );
}
