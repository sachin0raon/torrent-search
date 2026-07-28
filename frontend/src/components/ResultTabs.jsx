import { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { RefreshCw, Zap } from 'lucide-react';
import CopyButton from './CopyButton.jsx';
import StreamPanel from './StreamPanel.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import ForumTopicRow from './ForumTopicRow.jsx';
import { spring, staggerContainer, staggerItem, collapsePanel } from '../motion.js';

// Shared plain-glass retry control for per-source failures (Torrentio / Forum).
function RetryButton({ onRetry, retrying }) {
  if (!onRetry) return null;
  return (
    <button onClick={onRetry} disabled={retrying} style={{ marginTop: 2 }}>
      {!retrying && <RefreshCw size={13} />}
      {retrying ? 'Retrying…' : 'Retry'}
    </button>
  );
}

function TorrentioRow({ item }) {
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

function TorrentioTab({ result, onRetry, retrying }) {
  if (!result.ok) {
    return (
      <div>
        <ErrorBanner message={`Torrentio: ${result.error}`} />
        <RetryButton onRetry={onRetry} retrying={retrying} />
      </div>
    );
  }
  if (!result.items.length) {
    return <div className="empty">No torrentio results.</div>;
  }
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate">
      {result.items.map((item) => (
        <TorrentioRow key={item.magnet} item={item} />
      ))}
    </motion.div>
  );
}

function ForumTab({ result, onRetry, retrying }) {
  if (!result.ok) {
    return (
      <div>
        <ErrorBanner message={`Forum: ${result.error}`} />
        <RetryButton onRetry={onRetry} retrying={retrying} />
      </div>
    );
  }
  if (!result.items.length) {
    return <div className="empty">No forum results.</div>;
  }
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

export default function ResultTabs({ streams, onRetry, retrying, forumOnly = false }) {
  const [tab, setTab] = useState(forumOnly ? 'forum' : 'torrentio');

  // Forum-only search (no title/imdb): render just the forum results — no
  // Torrentio tab, since there's nothing to search it with.
  if (forumOnly) {
    return <ForumTab result={streams.forum} onRetry={onRetry} retrying={retrying} />;
  }

  const tCount = streams.torrentio.ok ? streams.torrentio.items.length : 0;
  const fCount = streams.forum.ok ? streams.forum.items.length : 0;

  return (
    <div>
      <div className="tabs" role="tablist">
        <button
          role="tab"
          className={`tab ${tab === 'torrentio' ? 'active' : ''}`}
          aria-selected={tab === 'torrentio'}
          onClick={() => setTab('torrentio')}
        >
          Torrentio<span className="count">{tCount}</span>
        </button>
        <button
          role="tab"
          className={`tab ${tab === 'forum' ? 'active' : ''}`}
          aria-selected={tab === 'forum'}
          onClick={() => setTab('forum')}
        >
          Forum<span className="count">{fCount}</span>
        </button>
      </div>

      {/* Keyed panel re-mounts on tab change: new content is in the DOM immediately,
          only the entrance animates (no exit) so it stays interaction/test friendly. */}
      <motion.div
        key={tab}
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={spring}
      >
        {tab === 'torrentio' ? (
          <TorrentioTab result={streams.torrentio} onRetry={onRetry} retrying={retrying} />
        ) : (
          <ForumTab result={streams.forum} />
        )}
      </motion.div>
    </div>
  );
}
