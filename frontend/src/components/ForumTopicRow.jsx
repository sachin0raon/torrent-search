import { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { api } from '../api/client.js';
import CopyButton from './CopyButton.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import { spring } from '../motion.js';

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
            {expanded ? 'Hide links' : 'Show links'}
          </button>
        </div>
      </div>

      {/* Error lives outside the collapsible panel so it stays visible after the
          row auto-collapses on failure. */}
      <ErrorBanner message={error} onDismiss={dismissError} />

      <AnimatePresence initial={false}>
        {expanded ? (
          <motion.div
            key="links"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={spring}
            style={{ overflow: 'hidden' }}
          >
            <div className="forum-links">
              {loading ? <div className="spinner">Loading links…</div> : null}
              {links && links.length === 0 && !loading ? (
                <div className="empty">No links found on topic page.</div>
              ) : null}
              {links &&
                links.map((l, i) => (
                  <div className="forum-link-row" key={i}>
                    <span className="fname">{l.filename || '(unnamed)'}</span>
                    <div className="actions">
                      {l.file_url ? (
                        <a href={l.file_url} target="_blank" rel="noreferrer">
                          <button>Torrent file</button>
                        </a>
                      ) : null}
                      {l.magnet ? (
                        <CopyButton value={l.magnet} />
                      ) : (
                        <span className="notice">no magnet</span>
                      )}
                    </div>
                  </div>
                ))}
            </div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
