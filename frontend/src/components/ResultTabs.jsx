import { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { RefreshCw, Zap } from 'lucide-react';
import CopyButton from './CopyButton.jsx';
import StreamPanel from './StreamPanel.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import ForumTopicRow from './ForumTopicRow.jsx';
import { spring, staggerContainer, staggerItem, collapsePanel } from '../motion.js';

// Torrent sources (Comet/Meteor/Torrentio) share this row shape once parsed;
// Forum's row (ForumTopicRow) is genuinely different, so it stays separate.
const TAB_ORDER = [
  { key: 'comet', label: 'Comet' },
  { key: 'meteor', label: 'Meteor' },
  { key: 'forum', label: 'Forum' },
  { key: 'torrentio', label: 'Torrentio' },
];

// Shared plain-glass retry control for per-source failures.
function RetryButton({ onRetry, retrying }) {
  if (!onRetry) return null;
  return (
    <button onClick={onRetry} disabled={retrying} style={{ marginTop: 2 }}>
      {!retrying && <RefreshCw size={13} />}
      {retrying ? 'Retrying…' : 'Retry'}
    </button>
  );
}

// Loading/error/empty branches shared by every source's tab. Returns null
// when the caller should render the actual items (ok, with results).
function renderSourceState(result, sourceLabel, onRetry) {
  // Only the very first fetch for a source has loading=true with no error yet
  // (a retry-in-flight keeps the prior error around — see App.jsx's
  // fetchTorrentSource/fetchForumSource — so it falls through to the error
  // branch below instead, showing "Retrying…" on the existing banner).
  if (result.loading && result.error == null) {
    return <div className="spinner">Fetching {sourceLabel}…</div>;
  }
  // Guard on an actual error message (not just !ok) so an untouched/idle
  // result — e.g. { ok: false, error: null, items: [] } — falls through to
  // the empty-state branch below instead of rendering "{label}: null".
  if (!result.ok && result.error) {
    return (
      <div>
        <ErrorBanner message={`${sourceLabel}: ${result.error}`} />
        <RetryButton onRetry={onRetry} retrying={result.loading} />
      </div>
    );
  }
  if (!result.items.length) {
    return <div className="empty">No {sourceLabel.toLowerCase()} results.</div>;
  }
  return null;
}

function TorrentRow({ item }) {
  const [streamOpen, setStreamOpen] = useState(false);
  return (
    <motion.div className="result-row" variants={staggerItem} whileHover={{ y: -2 }}>
      <div className="row-main">
        <div style={{ minWidth: 0 }}>
          <div className="name">{item.title}</div>
          <div className="stats">
            <span>👤 {item.seeders ?? '—'}</span>
            {item.size ? <span>💾 {item.size}</span> : null}
            {item.source ? <span>⚙️ {item.source}</span> : null}
          </div>
        </div>
        <div className="actions">
          <button onClick={() => setStreamOpen((o) => !o)} disabled={!item.magnet}>
            <Zap size={13} />
            {streamOpen ? 'Hide stream' : 'Stream'}
          </button>
          <CopyButton value={item.magnet} className="" />
        </div>
      </div>
      <AnimatePresence initial={false}>
        {streamOpen && (
          <motion.div key="stream" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <StreamPanel magnet={item.magnet} />
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function TorrentTab({ result, sourceLabel, onRetry }) {
  const state = renderSourceState(result, sourceLabel, onRetry);
  if (state) return state;
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate">
      {result.items.map((item) => (
        <TorrentRow key={item.magnet} item={item} />
      ))}
    </motion.div>
  );
}

function ForumTab({ result, onRetry }) {
  const state = renderSourceState(result, 'Forum', onRetry);
  if (state) return state;
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate">
      {result.items.map((item) => (
        <motion.div key={item.tid} variants={staggerItem}>
          <ForumTopicRow item={item} />
        </motion.div>
      ))}
    </motion.div>
  );
}

export default function ResultTabs({
  sources,
  forumOnly = false,
  onRetryTorrentio,
  onRetryComet,
  onRetryMeteor,
  onRetryForum,
}) {
  const [tab, setTab] = useState(forumOnly ? 'forum' : 'comet');

  // Forum-only search (no title/imdb): render just the forum results — no
  // other tabs, since there's nothing to search them with.
  if (forumOnly) {
    return <ForumTab result={sources.forum} onRetry={onRetryForum} />;
  }

  const retryFns = {
    comet: onRetryComet,
    meteor: onRetryMeteor,
    forum: onRetryForum,
    torrentio: onRetryTorrentio,
  };
  const activeLabel = TAB_ORDER.find((t) => t.key === tab).label;

  return (
    <div>
      <div className="tabs" role="tablist">
        {TAB_ORDER.map(({ key, label }) => {
          const result = sources[key];
          const count = result?.ok ? result.items.length : 0;
          return (
            <button
              key={key}
              role="tab"
              className={`tab ${tab === key ? 'active' : ''}`}
              aria-selected={tab === key}
              onClick={() => setTab(key)}
            >
              {label}<span className="count">{count}</span>
            </button>
          );
        })}
      </div>

      {/* Keyed panel re-mounts on tab change: new content is in the DOM immediately,
          only the entrance animates (no exit) so it stays interaction/test friendly. */}
      <motion.div
        key={tab}
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={spring}
      >
        {tab === 'forum' ? (
          <ForumTab result={sources.forum} onRetry={onRetryForum} />
        ) : (
          <TorrentTab result={sources[tab]} sourceLabel={activeLabel} onRetry={retryFns[tab]} />
        )}
      </motion.div>
    </div>
  );
}
