import { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { ChevronDown, ChevronUp, Zap, Download } from 'lucide-react';
import { api } from '../api/client.js';
import CopyButton from './CopyButton.jsx';
import StreamPanel from './StreamPanel.jsx';
import DownloadPanel from './DownloadPanel.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import { useDownloadsEnabled } from '../downloadCapabilityContext.jsx';
import { useStreamingEnabled } from '../streamingCapabilityContext.jsx';
import { collapsePanel } from '../motion.js';

// One link row inside an expanded forum topic. Owns its own stream-open state.
function ForumLinkRow({ link }) {
  const [streamOpen, setStreamOpen] = useState(false);
  const [downloadOpen, setDownloadOpen] = useState(false);
  const downloadsEnabled = useDownloadsEnabled();
  const streamingEnabled = useStreamingEnabled();

  function toggleStream() {
    setDownloadOpen(false);
    setStreamOpen((o) => !o);
  }
  function toggleDownload() {
    setStreamOpen(false);
    setDownloadOpen((o) => !o);
  }

  return (
    <div>
      <div className="forum-link-row">
        <span className="fname">{link.filename || '(unnamed)'}</span>
        <div className="actions">
          {link.file_url ? (
            <a href={link.file_url} target="_blank" rel="noreferrer">
              <button>Torrent file</button>
            </a>
          ) : null}
          {link.magnet ? (
            <>
              {streamingEnabled ? (
                <button onClick={toggleStream}>
                  <Zap size={13} />
                  {streamOpen ? 'Hide stream' : 'Stream'}
                </button>
              ) : null}
              {downloadsEnabled ? (
                <button onClick={toggleDownload}>
                  <Download size={13} />
                  {downloadOpen ? 'Hide download' : 'Download'}
                </button>
              ) : null}
              <CopyButton value={link.magnet} className="action-copy" />
            </>
          ) : (
            <span className="notice">no magnet</span>
          )}
        </div>
      </div>
      <AnimatePresence initial={false}>
        {streamOpen && (
          <motion.div key="stream" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <StreamPanel magnet={link.magnet} />
          </motion.div>
        )}
        {downloadOpen && (
          <motion.div key="download" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <DownloadPanel magnet={link.magnet} />
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// A forum search result. Its file/magnet links are fetched lazily when expanded
// (a second request that parses the topic HTML page).
export default function ForumTopicRow({ item }) {
  const [expanded, setExpanded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [links, setLinks] = useState(null);
  const [error, setError] = useState('');

  async function toggle() {
    const next = !expanded;
    setExpanded(next);
    if (next && links === null && !loading) {
      setLoading(true);
      setError('');
      try {
        const data = await api.forumTopic(item.topic_url);
        setLinks(data.links);
      } catch (e) {
        // Collapse on failure so the button returns to "Show links" and the
        // error surfaces as a dismissible banner; links stays null so re-opening
        // retries the fetch.
        setError(e.message || 'Failed to load topic');
        setExpanded(false);
      } finally {
        setLoading(false);
      }
    }
  }

  function dismissError() {
    setError('');
  }

  return (
    <div className="result-row">
      <div className="row-main">
        <a
          className="name topic-link"
          href={item.topic_url}
          target="_blank"
          rel="noreferrer"
          title="Open forum topic in a new tab"
        >
          {item.title}
          <svg className="ext-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor"
            strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M14 5h5v5M19 5l-8 8M12 5H6a1 1 0 0 0-1 1v12a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-6" />
          </svg>
        </a>
        <div className="actions">
          <button onClick={toggle} aria-expanded={expanded}>
            {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
            {expanded ? 'Hide links' : 'Show links'}
          </button>
        </div>
      </div>

      {/* Error lives outside the collapsible panel so it stays visible after the
          row auto-collapses on failure. */}
      <ErrorBanner message={error} onDismiss={dismissError} />

      <AnimatePresence initial={false}>
        {expanded ? (
          <motion.div key="links" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <div className="forum-links">
              {loading ? <div className="spinner">Loading links…</div> : null}
              {links && links.length === 0 && !loading ? (
                <div className="empty">No torrent or magnet links found on topic page.</div>
              ) : null}
              {links
                ? links.map((l, i) => <ForumLinkRow key={i} link={l} />)
                : null}
            </div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
