import { useEffect, useState } from 'react';
import { AnimatePresence, MotionConfig, motion } from 'framer-motion';
import { api } from './api/client.js';
import * as clientTorrentio from './api/torrentio.js';
import { getTorrentioMode } from './torrentioMode.js';
import SearchBar from './components/SearchBar.jsx';
import TitleList from './components/TitleList.jsx';
import SeasonEpisodePicker from './components/SeasonEpisodePicker.jsx';
import ResultTabs from './components/ResultTabs.jsx';
import SettingsModal from './components/SettingsModal.jsx';
import Toast from './components/Toast.jsx';
import { fadeUp, spring } from './motion.js';

// Wizard stages: search -> select title -> (tv) pick episode -> streams.
export default function App() {
  const [rawQuery, setRawQuery] = useState('');
  const [titles, setTitles] = useState(null);
  const [selected, setSelected] = useState(null); // TitleResult
  const [imdbId, setImdbId] = useState(undefined); // undefined=unknown, null=absent
  const [streams, setStreams] = useState(null);
  const [lastStreamReq, setLastStreamReq] = useState(null); // params of the last /api/streams fetch, for retry
  const [loading, setLoading] = useState(''); // '', 'search', 'resolve', 'streams'
  const [error, setError] = useState('');
  const [showSettings, setShowSettings] = useState(false);

  // Auto-dismiss the error toast after a few seconds.
  useEffect(() => {
    if (!error) return undefined;
    const t = setTimeout(() => setError(''), 5000);
    return () => clearTimeout(t);
  }, [error]);

  function resetBelowSearch() {
    setTitles(null);
    setSelected(null);
    setImdbId(undefined);
    setStreams(null);
    setError('');
  }

  function clearSelection() {
    setSelected(null);
    setStreams(null);
    setImdbId(undefined);
  }

  async function onSearch(q) {
    resetBelowSearch();
    setRawQuery(q);
    setLoading('search');
    try {
      const data = await api.searchTitles(q);
      setTitles(data.results);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading('');
    }
  }

  async function onSelectTitle(title) {
    setSelected(title);
    setStreams(null);
    setImdbId(undefined);
    setLoading('resolve');

    // Resolve imdb_id, but tolerate failure: the forum search only needs the raw
    // query, so we still fetch streams (torrentio is simply skipped without an id).
    let imdb = null;
    try {
      const res = await api.externalIds(title.media_type, title.tmdb_id);
      imdb = res.imdb_id;
    } catch (e) {
      setError(`Couldn't resolve IMDb ID: ${e.message}. Torrentio skipped; forum search will still run.`);
    }
    setImdbId(imdb);
    setLoading('');

    // Movies fetch immediately; TV waits for the season/episode picker.
    if (title.media_type === 'movie') {
      await fetchStreams(title, imdb, {});
    }
  }

  async function fetchStreams(title, imdb_id, { season, episode }) {
    // Remember the request so the Torrentio "Retry" button can re-run the same
    // combined /api/streams fetch without needing the picker again.
    setLastStreamReq({ title, imdb_id, season, episode });
    setLoading('streams');
    try {
      const serverPromise = api.streams({
        imdbId: imdb_id ?? undefined,
        mediaType: title.media_type,
        rawQuery,
        season,
        episode,
      });

      if (getTorrentioMode() === 'client') {
        // Client mode: fetch Torrentio from the browser (residential IP) and take
        // Forum from the backend's combined response. The server still runs its
        // own Torrentio call, but we discard it — this keeps the backend untouched.
        const [serverData, torrentio] = await Promise.all([
          serverPromise,
          clientTorrentio.fetchStreams({
            imdbId: imdb_id ?? undefined,
            mediaType: title.media_type,
            season,
            episode,
          }),
        ]);
        setStreams({ torrentio, forum: serverData.forum });
      } else {
        setStreams(await serverPromise);
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

  const isTvSelected = selected?.media_type === 'tv';

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
        <h2>Find any torrent, beautifully.</h2>
        <p>Search a movie or show, and we’ll gather sources side by side — with one-click magnet copy.</p>
      </section>

      <SearchBar onSearch={onSearch} disabled={loading === 'search'} />

      {loading === 'search' ? <div className="spinner">Searching titles…</div> : null}

      {/* Title list is hidden once a title is selected, so results sit right below. */}
      <AnimatePresence mode="wait">
        {titles && !selected ? (
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
              <SeasonEpisodePicker onFetch={(se) => fetchStreams(selected, imdbId, se)} />
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

      <Toast message={error} onDismiss={() => setError('')} />

      <AnimatePresence>
        {showSettings ? (
          <SettingsModal onClose={() => setShowSettings(false)} onSaved={() => {}} />
        ) : null}
      </AnimatePresence>
    </div>
    </MotionConfig>
  );
}
