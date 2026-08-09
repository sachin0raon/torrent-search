import { useEffect, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { RefreshCw, Zap, Download } from 'lucide-react';
import CopyButton from './CopyButton.jsx';
import StreamPanel from './StreamPanel.jsx';
import DownloadPanel from './DownloadPanel.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import ForumTopicRow from './ForumTopicRow.jsx';
import X1337Row from './X1337Row.jsx';
import { useDownloadsEnabled } from '../downloadCapabilityContext.jsx';
import { staggerContainer, staggerItem, collapsePanel } from '../motion.js';

// Torrent sources share row shapes; Forum and 1337x maintain dedicated rows.
const TAB_ORDER = [
  { key: 'comet', label: 'Comet' },
  { key: 'meteor', label: 'Meteor' },
  { key: 'forum', label: 'Forum' },
  { key: 'x1337', label: '1337x' },
  { key: 'torrentio', label: 'Torrentio' },
];

// Shared plain-glass retry control for per-source failures. Starting a fetch
// (App.jsx's fetchTorrentSource/fetchForumSource) always clears the prior
// error before setting loading, so by the time this renders the fetch is
// never in flight — no separate "Retrying…" state needed.
function RetryButton({ onRetry }) {
  if (!onRetry) return null;
  return (
    <button onClick={onRetry} style={{ marginTop: 2 }}>
      <RefreshCw size={13} />
      Retry
    </button>
  );
}

// Resets to false whenever a genuinely new error shows up (a fresh failure,
// possibly with the same message as a previously-dismissed one, always passes
// through `null` while the retry is in flight — see App.jsx — so this effect
// re-fires and un-dismisses it).
function useDismissedError(errorMessage) {
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    setDismissed(false);
  }, [errorMessage]);
  return [dismissed, () => setDismissed(true)];
}

// Loading/error/empty branches shared by every source's tab. Returns null
// when the caller should render the actual items (ok, with results).
function renderSourceState(result, sourceLabel, onRetry, dismissed, onDismiss) {
  if (result.loading) {
    return <div className="spinner">Fetching {sourceLabel}…</div>;
  }
  // Guard on an actual error message (not just !ok) so an untouched/idle
  // result — e.g. { ok: false, error: null, items: [] } — falls through to
  // the empty-state branch below instead of rendering "{label}: null".
  if (!result.ok && result.error) {
    return (
      <div>
        {!dismissed && (
          <ErrorBanner message={`${sourceLabel}: ${result.error}`} onDismiss={onDismiss} />
        )}
        <RetryButton onRetry={onRetry} />
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
  const [downloadOpen, setDownloadOpen] = useState(false);
  const downloadsEnabled = useDownloadsEnabled();

  function toggleStream() {
    setDownloadOpen(false);
    setStreamOpen((o) => !o);
  }
  function toggleDownload() {
    setStreamOpen(false);
    setDownloadOpen((o) => !o);
  }

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
          <button onClick={toggleStream} disabled={!item.magnet}>
            <Zap size={13} />
            {streamOpen ? 'Hide stream' : 'Stream'}
          </button>
          {downloadsEnabled ? (
            <button onClick={toggleDownload} disabled={!item.magnet}>
              <Download size={13} />
              {downloadOpen ? 'Hide download' : 'Download'}
            </button>
          ) : null}
          <CopyButton value={item.magnet} className="action-copy" />
        </div>
      </div>
      <AnimatePresence initial={false}>
        {streamOpen && (
          <motion.div key="stream" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <StreamPanel magnet={item.magnet} />
          </motion.div>
        )}
        {downloadOpen && (
          <motion.div key="download" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <DownloadPanel magnet={item.magnet} />
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

function TorrentTab({ result, sourceLabel, onRetry }) {
  const [dismissed, dismiss] = useDismissedError(result.error);
  const state = renderSourceState(result, sourceLabel, onRetry, dismissed, dismiss);
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
  const [dismissed, dismiss] = useDismissedError(result.error);
  const state = renderSourceState(result, 'Forum', onRetry, dismissed, dismiss);
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

function X1337Tab({ result, onRetry }) {
  const [dismissed, dismiss] = useDismissedError(result?.error);
  if (!result) return null;
  const state = renderSourceState(result, '1337x', onRetry, dismissed, dismiss);
  if (state) return state;
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate">
      {result.items.map((item) => (
        <X1337Row key={item.detail_path} item={item} />
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
  onRetryX1337,
}) {
  const [tab, setTab] = useState(forumOnly ? 'forum' : 'comet');

  const visibleTabs = forumOnly
    ? TAB_ORDER.filter((t) => t.key === 'forum' || t.key === 'x1337')
    : TAB_ORDER;

  const retryFns = {
    comet: onRetryComet,
    meteor: onRetryMeteor,
    forum: onRetryForum,
    x1337: onRetryX1337,
    torrentio: onRetryTorrentio,
  };
  const activeLabel = visibleTabs.find((t) => t.key === tab)?.label || '1337x';

  return (
    <div>
      <div className="tabs" role="tablist">
        {visibleTabs.map(({ key, label }) => {
          const result = sources?.[key];
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

      <div key={tab}>
        {tab === 'forum' ? (
          <ForumTab result={sources?.forum} onRetry={onRetryForum} />
        ) : tab === 'x1337' ? (
          <X1337Tab result={sources?.x1337} onRetry={onRetryX1337} />
        ) : (
          <TorrentTab result={sources?.[tab]} sourceLabel={activeLabel} onRetry={retryFns[tab]} />
        )}
      </div>
    </div>
  );
}
