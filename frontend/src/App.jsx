import { useEffect, useState } from 'react';
import { useSessions } from './sessionContext.jsx';
import { useDownloadsEnabled } from './downloadCapabilityContext.jsx';
import { useStreamingEnabled } from './streamingCapabilityContext.jsx';
import { AnimatePresence, MotionConfig, motion } from 'framer-motion';
import { HardDriveDownload, FolderOpen } from 'lucide-react';
import { api } from './api/client.js';
import * as clientTorrentio from './api/torrentio.js';
import * as clientComet from './api/comet.js';
import * as clientMeteor from './api/meteor.js';
import { getTorrentioMode } from './torrentioMode.js';
import { getCometMode } from './cometMode.js';
import { getMeteorMode } from './meteorMode.js';
import { getActiveDiscoverBadge, setActiveDiscoverBadge } from './discoverSectionState.js';
import SearchBar from './components/SearchBar.jsx';
import DiscoverSection from './components/DiscoverSection.jsx';
import TitleList from './components/TitleList.jsx';
import SeasonEpisodePicker from './components/SeasonEpisodePicker.jsx';
import ResultTabs from './components/ResultTabs.jsx';
import FilterBar from './components/FilterBar.jsx';
import SettingsModal from './components/SettingsModal.jsx';
import StatsModal from './components/StatsModal.jsx';
import DownloadsModal from './components/DownloadsModal.jsx';
import Toast from './components/Toast.jsx';
import ScrollToTopButton from './components/ScrollToTopButton.jsx';
import { fadeUp, spring } from './motion.js';
import { useFilterState, applyFilters } from './filterState.js';

// Each torrent source is fetched independently (own backend endpoint, own
// client-side module, own client/server mode toggle), so one slow/failed
// source never blocks or affects the others' results or retry.
const TORRENT_SOURCES = {
  torrentio: { backendCall: api.torrentio, clientModule: clientTorrentio, modeGetter: getTorrentioMode },
  comet: { backendCall: api.comet, clientModule: clientComet, modeGetter: getCometMode },
  meteor: { backendCall: api.meteor, clientModule: clientMeteor, modeGetter: getMeteorMode },
};

// Wizard stages: search -> select title -> (tv) pick episode -> streams.
export default function App() {
  const [rawQuery, setRawQuery] = useState('');
  const [titles, setTitles] = useState(null);
  const [selected, setSelected] = useState(null); // TitleResult
  const [imdbId, setImdbId] = useState(undefined); // undefined=unknown, null=absent
  const [seasons, setSeasons] = useState(null); // TMDB seasons for the selected TV show
  // Per-source results: { comet, meteor, torrentio, forum } each
  // { ok, error, items, loading }. null until the first fetch for a title.
  const [sources, setSources] = useState(null);
  const [lastStreamReq, setLastStreamReq] = useState(null); // params of the last fetch, for retry
  const [loading, setLoading] = useState(''); // '', 'search', 'resolve'
  const [error, setError] = useState('');
  const [info, setInfo] = useState(''); // non-error notice (e.g. forum URL auto-updated)
  const [showSettings, setShowSettings] = useState(false);
  const [showStats, setShowStats] = useState(false);
  const [showDownloads, setShowDownloads] = useState(false);
  const downloadsEnabled = useDownloadsEnabled();
  const streamingEnabled = useStreamingEnabled();
  const [fileBrowserUrl, setFileBrowserUrl] = useState('');
  const [forumOnly, setForumOnly] = useState(false); // forum-only search (no TMDB title)
  const [tmdbFailed, setTmdbFailed] = useState(false); // last title search errored
  const [discoverActive, setDiscoverActive] = useState(getActiveDiscoverBadge); // active Discover badge key, or null
  const [discoverActiveBeforeSelect, setDiscoverActiveBeforeSelect] = useState(null); // restored by "Change title"
  const { sessions } = useSessions();
  const { resolution, language, setResolution, setLanguage } = useFilterState();

  function toggleDiscoverBadge(key) {
    const next = discoverActive === key ? null : key;
    setDiscoverActive(next);
    setActiveDiscoverBadge(next);
  }

  // Fetch once on mount — file_browser_url is a deploy-time env value that never changes.
  useEffect(() => {
    api.getConfig().then((c) => {
      if (c?.file_browser_url) setFileBrowserUrl(c.file_browser_url);
    }).catch(() => {});
  }, []);

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

  // Surface an auto-updated forum base URL from a forum search response.
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
    setSources(null);
    setForumOnly(false);
    setTmdbFailed(false);
    setLastStreamReq(null);
    setError('');
  }

  function clearSelection() {
    setSelected(null);
    setSources(null);
    setImdbId(undefined);
    setSeasons(null);
    setDiscoverActive(discoverActiveBeforeSelect); // "← Change title" undoes the auto-hide from selecting
  }

  async function onSearch(q) {
    resetBelowSearch();
    setDiscoverActive(null);
    setActiveDiscoverBadge(null);
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
    setSources(null);
    setImdbId(undefined);
    setSeasons(null);
    setForumOnly(false);
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
    // movie path below (which fetches streams synchronously) passes `query`
    // explicitly rather than relying on fetchAllSources reading stale state.
    const query = fromDiscover ? title.title : rawQuery.trim() || title.title;
    setRawQuery(query);

    // Resolve imdb_id, but tolerate failure: the forum search only needs the raw
    // query, so we still fetch streams (torrent sources simply skip without an id).
    // For TV, fetch the season/episode list in parallel to populate the dropdowns.
    let imdb = null;
    if (title.media_type === 'tv') {
      const [idRes, seasonsRes] = await Promise.allSettled([
        api.externalIds(title.media_type, title.tmdb_id),
        api.tvSeasons(title.tmdb_id),
      ]);
      if (idRes.status === 'fulfilled') imdb = idRes.value.imdb_id;
      else setError(`Couldn't resolve IMDb ID: ${idRes.reason.message}. Torrent sources skipped; forum search will still run.`);
      // If seasons fetch fails, leave null -> the picker falls back to manual inputs.
      if (seasonsRes.status === 'fulfilled') setSeasons(seasonsRes.value.seasons);
    } else {
      try {
        const res = await api.externalIds(title.media_type, title.tmdb_id);
        imdb = res.imdb_id;
      } catch (e) {
        setError(`Couldn't resolve IMDb ID: ${e.message}. Torrent sources skipped; forum search will still run.`);
      }
    }
    setImdbId(imdb);
    setLoading('');

    // Movies fetch immediately; TV waits for the season/episode picker.
    if (title.media_type === 'movie') {
      fetchAllSources(title, imdb, {}, query);
    }
  }

  // Kick off one torrent source's fetch (Torrentio/Comet/Meteor). Independent
  // of the other sources: sets only this source's slice of `sources`, using
  // whichever mode (client/server) that source's own toggle currently selects.
  async function fetchTorrentSource(key, { imdbId, mediaType, season, episode }) {
    const { backendCall, clientModule, modeGetter } = TORRENT_SOURCES[key];
    // Hard reset (not a merge with the prior result): starting a fetch — first
    // attempt or Retry — always clears any previous error so the tab shows the
    // loading spinner, not a stale error banner, while it's in flight.
    setSources((prev) => ({ ...prev, [key]: { ok: false, error: null, items: [], loading: true } }));
    try {
      const result =
        modeGetter() === 'client'
          ? await clientModule.fetchStreams({ imdbId, mediaType, season, episode })
          : await backendCall({ imdbId, mediaType, season, episode });
      setSources((prev) => ({ ...prev, [key]: { ...result, loading: false } }));
    } catch (e) {
      setSources((prev) => ({
        ...prev,
        [key]: { ok: false, error: e.message, items: [], loading: false },
      }));
    }
  }

  // Forum has no client-mode analog (HTML scraping, backend-only) and takes a
  // raw query rather than an imdb_id.
  async function fetchForumSource(query) {
    setSources((prev) => ({ ...prev, forum: { ok: false, error: null, items: [], loading: true } }));
    try {
      const data = await api.forumSearch({ rawQuery: query });
      noteForumUpdate(data);
      setSources((prev) => ({
        ...prev,
        forum: { ok: data.ok, error: data.error, items: data.items, loading: false },
      }));
    } catch (e) {
      setSources((prev) => ({
        ...prev,
        forum: { ok: false, error: e.message, items: [], loading: false },
      }));
    }
  }

  async function fetchX1337Source(query) {
    setSources((prev) => ({ ...prev, x1337: { ok: false, error: null, items: [], loading: true } }));
    try {
      const data = await api.x1337Search({ q: query });
      setSources((prev) => ({
        ...prev,
        x1337: { ok: data.ok, error: data.error, items: data.items, loading: false },
      }));
    } catch (e) {
      setSources((prev) => ({
        ...prev,
        x1337: { ok: false, error: e.message, items: [], loading: false },
      }));
    }
  }

  // `queryOverride` is only needed by onSelectTitle's synchronous movie-path call
  // above, where a just-set rawQuery isn't visible yet in this render's closure.
  // Every other caller (season/episode picker, retry) runs after a re-render has
  // already picked up the current rawQuery, so they can omit it.
  function fetchAllSources(title, imdb_id, { season, episode }, queryOverride) {
    const query = queryOverride ?? rawQuery;
    // Remembered so each source's independent Retry can re-run the same request
    // without needing the picker again.
    setLastStreamReq({ title, imdb_id, season, episode, query });
    const imdbId = imdb_id ?? undefined;
    const mediaType = title.media_type;
    // Fire all independently — no Promise.all gating the UI, so a fast
    // source's tab is browsable while a slower one is still loading.
    fetchTorrentSource('torrentio', { imdbId, mediaType, season, episode });
    fetchTorrentSource('comet', { imdbId, mediaType, season, episode });
    fetchTorrentSource('meteor', { imdbId, mediaType, season, episode });
    fetchForumSource(query);
    fetchX1337Source(query);
  }

  function retryTorrentSource(key) {
    if (!lastStreamReq) return;
    const { title, imdb_id, season, episode } = lastStreamReq;
    fetchTorrentSource(key, { imdbId: imdb_id ?? undefined, mediaType: title.media_type, season, episode });
  }

  function retryForum() {
    if (!lastStreamReq) return;
    fetchForumSource(lastStreamReq.query);
  }

  function retryX1337() {
    if (!lastStreamReq) return;
    fetchX1337Source(lastStreamReq.query);
  }

  // Forum & 1337x fallback search: no TMDB title / imdb_id needed.
  async function searchForumOnly() {
    const q = rawQuery.trim();
    if (!q) return;
    setSelected(null);
    setForumOnly(true);
    setSources(null);
    setLastStreamReq({ title: null, imdb_id: null, season: undefined, episode: undefined, query: q });
    fetchForumSource(q);
    fetchX1337Source(q);
  }

  function backToSearch() {
    setForumOnly(false);
    setSources(null);
  }

  const isTvSelected = selected?.media_type === 'tv';
  const filters = { resolution, language };
  const filteredSources = sources ? {
    comet:     sources.comet     ? { ...sources.comet,     items: applyFilters(sources.comet.items     || [], filters) } : undefined,
    meteor:    sources.meteor    ? { ...sources.meteor,    items: applyFilters(sources.meteor.items    || [], filters) } : undefined,
    torrentio: sources.torrentio ? { ...sources.torrentio, items: applyFilters(sources.torrentio.items || [], filters) } : undefined,
    x1337:     sources.x1337     ? { ...sources.x1337,     items: applyFilters(sources.x1337.items     || [], filters) } : undefined,
    forum:     sources.forum,
  } : null;
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
          <span className="eyebrow">TMDB · Comet · Meteor · Torrentio · Forum</span>
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
                  No IMDb ID found for this title — Comet/Meteor/Torrentio are skipped, but the forum search still runs.
                </div>
              ) : null}

              {isTvSelected && loading !== 'resolve' ? (
                <SeasonEpisodePicker
                  seasons={seasons}
                  onFetch={(se) => fetchAllSources(selected, imdbId, se)}
                />
              ) : null}

              {filteredSources ? (
                <>
                  <FilterBar
                    resolution={resolution}
                    language={language}
                    onResolutionChange={setResolution}
                    onLanguageChange={setLanguage}
                  />
                  <ResultTabs
                    sources={filteredSources}
                    onRetryTorrentio={() => retryTorrentSource('torrentio')}
                    onRetryComet={() => retryTorrentSource('comet')}
                    onRetryMeteor={() => retryTorrentSource('meteor')}
                    onRetryForum={retryForum}
                    onRetryX1337={retryX1337}
                  />
                </>
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
              <button onClick={searchForumOnly} disabled={sources?.forum?.loading}>
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
            {sources ? (
              <ResultTabs
                sources={sources}
                forumOnly
                onRetryForum={retryForum}
                onRetryX1337={retryX1337}
              />
            ) : null}
          </div>
        ) : null}

        <Toast message={error} onDismiss={() => setError('')} />
        <Toast variant="info" message={info} onDismiss={() => setInfo('')} />
        <ScrollToTopButton />

        <div className="right-fabs">
          {fileBrowserUrl ? (
            <a href={fileBrowserUrl} target="_blank" rel="noreferrer">
              <button className="stats-fab icon-btn" aria-label="File browser" title="File browser">
                <FolderOpen size={18} />
              </button>
            </a>
          ) : null}
          {downloadsEnabled ? (
            <button
              className="stats-fab icon-btn"
              onClick={() => setShowDownloads(true)}
              aria-label="Downloads"
              title="Downloads"
            >
              <HardDriveDownload size={18} />
            </button>
          ) : null}
          {streamingEnabled && sessions.size > 0 ? (
            <button
              className="stats-fab icon-btn"
              onClick={() => setShowStats(true)}
              aria-label="Active streams"
              title="Active streams"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"
                strokeLinecap="round" strokeLinejoin="round">
                <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
              </svg>
            </button>
          ) : null}
        </div>

        <AnimatePresence>
          {showSettings ? (
            <SettingsModal onClose={() => setShowSettings(false)} onSaved={() => { }} />
          ) : null}
          {showStats ? (
            <StatsModal onClose={() => setShowStats(false)} />
          ) : null}
          {showDownloads ? (
            <DownloadsModal onClose={() => setShowDownloads(false)} />
          ) : null}
        </AnimatePresence>
      </div>
    </MotionConfig>
  );
}
