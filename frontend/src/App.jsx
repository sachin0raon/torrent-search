import { useEffect, useState } from 'react';
import { AnimatePresence, MotionConfig, motion } from 'framer-motion';
import { api } from './api/client.js';
import * as clientTorrentio from './api/torrentio.js';
import { getTorrentioMode } from './torrentioMode.js';
import { getActiveDiscoverBadge, setActiveDiscoverBadge } from './discoverSectionState.js';
import SearchBar from './components/SearchBar.jsx';
import DiscoverSection from './components/DiscoverSection.jsx';
import TitleList from './components/TitleList.jsx';
import SeasonEpisodePicker from './components/SeasonEpisodePicker.jsx';
import ResultTabs from './components/ResultTabs.jsx';
import SettingsModal from './components/SettingsModal.jsx';
import Toast from './components/Toast.jsx';
import ScrollToTopButton from './components/ScrollToTopButton.jsx';
import { fadeUp, spring } from './motion.js';

// Wizard stages: search -> select title -> (tv) pick episode -> streams.
export default function App() {
  const [rawQuery, setRawQuery] = useState('');
  const [titles, setTitles] = useState(null);
  const [selected, setSelected] = useState(null); // TitleResult
  const [imdbId, setImdbId] = useState(undefined); // undefined=unknown, null=absent
  const [seasons, setSeasons] = useState(null); // TMDB seasons for the selected TV show
  const [streams, setStreams] = useState(null);
  const [lastStreamReq, setLastStreamReq] = useState(null); // params of the last /api/streams fetch, for retry
  const [loading, setLoading] = useState(''); // '', 'search', 'resolve', 'streams'
  const [error, setError] = useState('');
  const [info, setInfo] = useState(''); // non-error notice (e.g. forum URL auto-updated)
  const [showSettings, setShowSettings] = useState(false);
  const [forumOnly, setForumOnly] = useState(false); // forum-only search (no TMDB title)
  const [tmdbFailed, setTmdbFailed] = useState(false); // last title search errored
  const [discoverActive, setDiscoverActive] = useState(getActiveDiscoverBadge); // active Discover badge key, or null
  const [discoverActiveBeforeSelect, setDiscoverActiveBeforeSelect] = useState(null); // restored by "Change title"

  function toggleDiscoverBadge(key) {
    const next = discoverActive === key ? null : key;
    setDiscoverActive(next);
    setActiveDiscoverBadge(next);
  }

  // Auto-dismiss the error toast after a few seconds.
  useEffect(() => {
    if (!error) return undefined;
    const t = setTimeout(() => setError(''), 5000);
    return () => clearTimeout(t);
  }, [error]);

  // Info notices linger a little longer (the forum URL is worth reading).
  useEffect(() => {
    if (!info) return undefined;
    const t = setTimeout(() => setInfo(''), 7000);
    return () => clearTimeout(t);
  }, [info]);

  // Surface an auto-updated forum base URL from any /api/streams response.
  function noteForumUpdate(data) {
    if (data && data.forum_base_updated) {
      setInfo(`Forum URL auto-updated to ${data.forum_base_updated}`);
    }
  }

  function resetBelowSearch() {
    setTitles(null);
    setSelected(null);
    setImdbId(undefined);
    setSeasons(null);
    setStreams(null);
    setForumOnly(false);
    setTmdbFailed(false);
    setLastStreamReq(null);
    setError('');
  }

  function clearSelection() {
    setSelected(null);
    setStreams(null);
    setImdbId(undefined);
    setSeasons(null);
    setDiscoverActive(discoverActiveBeforeSelect); // "← Change title" undoes the auto-hide from selecting
  }

  async function onSearch(q) {
    resetBelowSearch();
    setRawQuery(q);
    setLoading('search');
    try {
      const data = await api.searchTitles(q);
      setTitles(data.results);
    } catch (e) {
      // TMDB is down/unreachable, but the forum search only needs the raw query —
      // surface a forum-only fallback rather than dead-ending.
      setTmdbFailed(true);
      setError(e.message);
    } finally {
      setLoading('');
    }
  }

  async function onSelectTitle(title, { fromDiscover = false } = {}) {
    setSelected(title);
    setStreams(null);
    setImdbId(undefined);
    setSeasons(null);
    setLoading('resolve');
    // Hide Discover's shown list once anything is picked (search or Discover),
    // but remember what was showing so "← Change title" can bring it back.
    setDiscoverActiveBeforeSelect(discoverActive);
    setDiscoverActive(null);

    // Discover cards have no typed search query behind them, so always use the
    // title's own name — and a *stale* query left over from an earlier, unrelated
    // search must not leak into a fresh Discover-originated selection. A real
    // search keeps using what the user typed (may deliberately differ from the
    // TMDB-resolved title — forum aliases etc., see Decision Log #11).
    // setRawQuery won't be visible in this closure until the next render, so the
    // movie path below (which calls fetchStreams synchronously) passes `query`
    // explicitly rather than relying on fetchStreams reading the stale state.
    const query = fromDiscover ? title.title : rawQuery.trim() || title.title;
    setRawQuery(query);

    // Resolve imdb_id, but tolerate failure: the forum search only needs the raw
    // query, so we still fetch streams (torrentio is simply skipped without an id).
    // For TV, fetch the season/episode list in parallel to populate the dropdowns.
    let imdb = null;
    if (title.media_type === 'tv') {
      const [idRes, seasonsRes] = await Promise.allSettled([
        api.externalIds(title.media_type, title.tmdb_id),
        api.tvSeasons(title.tmdb_id),
      ]);
      if (idRes.status === 'fulfilled') imdb = idRes.value.imdb_id;
      else setError(`Couldn't resolve IMDb ID: ${idRes.reason.message}. Torrentio skipped; forum search will still run.`);
      // If seasons fetch fails, leave null -> the picker falls back to manual inputs.
      if (seasonsRes.status === 'fulfilled') setSeasons(seasonsRes.value.seasons);
    } else {
      try {
        const res = await api.externalIds(title.media_type, title.tmdb_id);
        imdb = res.imdb_id;
      } catch (e) {
        setError(`Couldn't resolve IMDb ID: ${e.message}. Torrentio skipped; forum search will still run.`);
      }
    }
    setImdbId(imdb);
    setLoading('');

    // Movies fetch immediately; TV waits for the season/episode picker.
    if (title.media_type === 'movie') {
      await fetchStreams(title, imdb, {}, query);
    }
  }

  // `queryOverride` is only needed by onSelectTitle's synchronous movie-path call
  // above, where a just-set rawQuery isn't visible yet in this render's closure.
  // Every other caller (season/episode picker, retry) runs after a re-render has
  // already picked up the current rawQuery, so they can omit it.
  async function fetchStreams(title, imdb_id, { season, episode }, queryOverride) {
    const query = queryOverride ?? rawQuery;
    // Remember the request so the Torrentio "Retry" button can re-run the same
    // combined /api/streams fetch without needing the picker again.
    setLastStreamReq({ title, imdb_id, season, episode });
    setLoading('streams');
    const clientMode = getTorrentioMode() === 'client';
    try {
      const serverPromise = api.streams({
        imdbId: imdb_id ?? undefined,
        mediaType: title.media_type,
        rawQuery: query,
        season,
        episode,
        // Client mode fetches Torrentio from the browser (residential IP)
        // instead, so tell the backend to skip its own Torrentio call.
        skipTorrentio: clientMode,
      });

      if (clientMode) {
        const [serverData, torrentio] = await Promise.all([
          serverPromise,
          clientTorrentio.fetchStreams({
            imdbId: imdb_id ?? undefined,
            mediaType: title.media_type,
            season,
            episode,
          }),
        ]);
        noteForumUpdate(serverData);
        setStreams({ torrentio, forum: serverData.forum });
      } else {
        const serverData = await serverPromise;
        noteForumUpdate(serverData);
        setStreams(serverData);
      }
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading('');
    }
  }

  function retryStreams() {
    if (!lastStreamReq) return;
    const { title, imdb_id, season, episode } = lastStreamReq;
    fetchStreams(title, imdb_id, { season, episode });
  }

  // Forum-only search: no TMDB title / imdb_id needed. `media_type` is a required
  // param on /api/streams, so we pass a placeholder; torrentio is skipped server-side
  // (no imdb) and discarded here — only the forum half is shown.
  async function searchForumOnly() {
    const q = rawQuery.trim();
    if (!q) return;
    setSelected(null);
    setForumOnly(true);
    setStreams(null);
    setLoading('streams');
    try {
      const data = await api.streams({ mediaType: 'movie', rawQuery: q });
      noteForumUpdate(data);
      setStreams({ torrentio: null, forum: data.forum });
    } catch (e) {
      // Surface as an in-panel forum error so the Retry button stays available.
      setStreams({ torrentio: null, forum: { ok: false, error: e.message, items: [] } });
    } finally {
      setLoading('');
    }
  }

  function backToSearch() {
    setForumOnly(false);
    setStreams(null);
  }

  const isTvSelected = selected?.media_type === 'tv';

  // Offer a forum-only search when TMDB errored or returned nothing for a query.
  const showForumFallback =
    !!rawQuery &&
    !selected &&
    !forumOnly &&
    loading !== 'search' &&
    (tmdbFailed || (Array.isArray(titles) && titles.length === 0));

  return (
    <MotionConfig reducedMotion="user">
      <div className="container">
        <header className="app-header">
          <button
            className="icon-btn"
            onClick={() => setShowSettings(true)}
            aria-label="Settings"
            title="Settings"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"
              strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="3" />
              <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
            </svg>
          </button>
        </header>

        <section className="hero">
          <span className="eyebrow">TMDB · Torrentio · Forum</span>
          <h2>Find any torrent, magically.</h2>
          <p>Search a movie or show, and we’ll gather sources side by side — with one-click magnet copy.</p>
        </section>

        <SearchBar onSearch={onSearch} disabled={loading === 'search'} />

        <DiscoverSection
          onSelect={(title) => onSelectTitle(title, { fromDiscover: true })}
          active={discoverActive}
          onToggleBadge={toggleDiscoverBadge}
        />

        {loading === 'search' ? <div className="spinner">Searching titles…</div> : null}

        {/* Title list is hidden once a title is selected, so results sit right below. */}
        <AnimatePresence mode="wait">
          {titles && titles.length > 0 && !selected ? (
            <motion.div
              key="titles"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0, y: -8 }}
              transition={{ duration: 0.2 }}
            >
              <div className="section-title">Titles</div>
              <TitleList results={titles} onSelect={onSelectTitle} selectedId={null} />
            </motion.div>
          ) : null}

          {selected ? (
            <motion.div
              key="selected"
              variants={fadeUp}
              initial="initial"
              animate="animate"
              exit="exit"
              transition={spring}
            >
              <div className="section-title selected-bar">
                <span className="selected-name">
                  {selected.title} {selected.year ? `(${selected.year})` : ''}
                </span>
                <button className="link" onClick={clearSelection}>
                  ← Change title
                </button>
              </div>

              {loading === 'resolve' ? <div className="spinner">Resolving IMDb ID…</div> : null}

              {imdbId === null ? (
                <div className="notice">
                  No IMDb ID found for this title — torrentio is skipped, but the forum search still runs.
                </div>
              ) : null}

              {isTvSelected && loading !== 'resolve' ? (
                <SeasonEpisodePicker
                  seasons={seasons}
                  onFetch={(se) => fetchStreams(selected, imdbId, se)}
                />
              ) : null}

              {loading === 'streams' ? <div className="spinner">Fetching torrents…</div> : null}

              {streams ? (
                <ResultTabs
                  streams={streams}
                  onRetry={retryStreams}
                  retrying={loading === 'streams'}
                />
              ) : null}
            </motion.div>
          ) : null}
        </AnimatePresence>

        {/* TMDB failed or returned nothing — the forum doesn't need an IMDb ID,
            so offer a direct forum search on the raw query. */}
        {showForumFallback ? (
          <div className="empty">
            {tmdbFailed ? 'Title lookup failed.' : 'No matching titles found.'}{' '}
            You can still search the forum directly.
            <div style={{ marginTop: 14 }}>
              <button onClick={searchForumOnly} disabled={loading === 'streams'}>
                Search forum for “{rawQuery}”
              </button>
            </div>
          </div>
        ) : null}

        {forumOnly ? (
          <div>
            <div className="section-title selected-bar">
              <span className="selected-name">Forum results for “{rawQuery}”</span>
              <button className="link" onClick={backToSearch}>
                ← New search
              </button>
            </div>
            {loading === 'streams' ? <div className="spinner">Searching forum…</div> : null}
            {streams ? (
              <ResultTabs
                streams={streams}
                forumOnly
                onRetry={searchForumOnly}
                retrying={loading === 'streams'}
              />
            ) : null}
          </div>
        ) : null}

        <Toast message={error} onDismiss={() => setError('')} />
        <Toast variant="info" message={info} onDismiss={() => setInfo('')} />
        <ScrollToTopButton />

        <AnimatePresence>
          {showSettings ? (
            <SettingsModal onClose={() => setShowSettings(false)} onSaved={() => { }} />
          ) : null}
        </AnimatePresence>
      </div>
    </MotionConfig>
  );
}
