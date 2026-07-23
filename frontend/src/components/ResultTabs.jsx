import { useState } from 'react';
import { motion } from 'framer-motion';
import CopyButton from './CopyButton.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import ForumTopicRow from './ForumTopicRow.jsx';
import { spring, staggerContainer, staggerItem } from '../motion.js';

function TorrentioRow({ item }) {
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
          <CopyButton value={item.magnet} className="" />
        </div>
      </div>
    </motion.div>
  );
}

function TorrentioTab({ result, onRetry, retrying }) {
  if (!result.ok) {
    return (
      <div>
        <ErrorBanner message={`Torrentio: ${result.error}`} />
        {onRetry ? (
          <button onClick={onRetry} disabled={retrying} style={{ marginTop: 2 }}>
            {retrying ? 'Retrying…' : 'Retry'}
          </button>
        ) : null}
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

function ForumTab({ result }) {
  if (!result.ok) {
    return <ErrorBanner message={`Forum: ${result.error}`} />;
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

export default function ResultTabs({ streams, onRetry, retrying }) {
  const [tab, setTab] = useState('torrentio');
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
