import { memo, useEffect, useState } from 'react';
import { api } from '../api/client.js';
import TitleCard from './TitleCard.jsx';
import ErrorBanner from './ErrorBanner.jsx';

const PAGE_SIZE = 10;

// One category+media_type list (e.g. Trending Movies), shown when its badge is
// active in DiscoverSection. Fetches its own page 1 on mount, appends further
// pages on "Load more". Failures are scoped to this list only.
//
// memo()'d: DiscoverSection keeps every previously-viewed rail mounted (just
// hidden) so switching badges doesn't re-fetch, but that means a badge click
// re-renders DiscoverSection and, without memo, every mounted DiscoverRail
// too — including ones sitting hidden behind display:none. Its props
// (category/mediaType/label/onSelect) are referentially stable across that
// re-render, so memo fully skips the unaffected ones.
function DiscoverRail({ category, mediaType, label, onSelect }) {
  const [items, setItems] = useState([]);
  const [page, setPage] = useState(0); // last successfully loaded page (0 = none yet)
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [hasMore, setHasMore] = useState(true);

  async function loadPage(nextPage) {
    setLoading(true);
    setError('');
    try {
      const data = await api.discover({ category, mediaType, page: nextPage });
      setItems((prev) => {
        if (nextPage === 1) return data.results;
        // TMDB's own list can reorder between two separately-fetched pages, so
        // an item already on an earlier page can reappear on a later one —
        // drop it here rather than rendering (and keying) it twice.
        const seen = new Set(prev.map((t) => `${t.media_type}-${t.tmdb_id}`));
        const fresh = data.results.filter((t) => !seen.has(`${t.media_type}-${t.tmdb_id}`));
        return [...prev, ...fresh];
      });
      setPage(nextPage);
      setHasMore(data.results.length === PAGE_SIZE);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadPage(1);
    // Re-fetch only if the rail's identity changes (it never does today, but
    // keeps the effect correct if rails become configurable later).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [category, mediaType]);

  const retryPage = page + 1;

  return (
    <div className="discover-rail" aria-label={label}>
      {error ? (
        <ErrorBanner message={error} onDismiss={() => setError('')} />
      ) : null}

      {items.length === 0 && loading ? (
        <div className="spinner">Loading…</div>
      ) : null}

      {items.length === 0 && !loading && error ? (
        <button onClick={() => loadPage(retryPage)}>Retry</button>
      ) : null}

      {items.length > 0 ? (
        <>
          <div className="title-list">
            {items.map((t) => (
              <TitleCard key={`${t.media_type}-${t.tmdb_id}`} title={t} onSelect={onSelect} />
            ))}
          </div>
          {hasMore ? (
            <div className="discover-load-more-wrap">
              <button onClick={() => loadPage(retryPage)} disabled={loading}>
                {loading ? 'Loading…' : 'Load more'}
              </button>
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

export default memo(DiscoverRail);
