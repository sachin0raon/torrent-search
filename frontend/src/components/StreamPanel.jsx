import { useEffect, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { ChevronDown, ChevronUp, RefreshCw } from 'lucide-react';
import { streamer } from '../api/streamer.js';
import { useSessions } from '../sessionContext.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import CopyButton from './CopyButton.jsx';
import { buildStreamUrl, playerLinks, baseName } from '../playerLinks.js';
import { collapsePanel } from '../motion.js';
import { formatSize } from '../formatSize.js';

// Rendered only when the row is expanded — defers URL/link building until needed.
function StreamableLinks({ session, file, name }) {
  const url = buildStreamUrl(session.sessionId, file.index, name);
  const links = playerLinks(url, name);
  return (
    <div className="player-links">
      {links.map((l) => (
        <a key={l.id} href={l.href} rel="noreferrer">
          <button className="player-btn">{l.label}</button>
        </a>
      ))}
      <CopyButton value={url} label="Copy stream URL" className="" />
    </div>
  );
}

function FileRow({ session, file }) {
  const [expanded, setExpanded] = useState(false);
  const name = baseName(file.path);

  if (!file.streamable) {
    const downloadUrl = buildStreamUrl(session.sessionId, file.index, name);
    return (
      <div className="stream-file-row">
        <div className="stream-file-head">
          <span className="fname">{name}</span>
          <div className="stream-file-actions">
            <span className="notice">{formatSize(file.size)}</span>
            <a href={downloadUrl} download={name} rel="noreferrer">
              <button className="player-btn">Download</button>
            </a>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="stream-file-row">
      <div className="stream-file-head">
        <span className="fname">{name}</span>
        <div className="stream-file-actions">
          <span className="notice">{formatSize(file.size)}</span>
          <button onClick={() => setExpanded((e) => !e)} aria-expanded={expanded}>
            {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
            {expanded ? 'Hide links' : 'Show links'}
          </button>
        </div>
      </div>
      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div key="links" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <StreamableLinks session={session} file={file} name={name} />
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// Inline streaming panel — fetches a session for the magnet and lists files
// with player deep links. Rendered directly inside result rows, no modal.
export default function StreamPanel({ magnet }) {
  const [session, setSession] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const { register, unregister } = useSessions();

  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    setLoading(true);
    setError('');
    streamer
      .createSession(magnet, controller.signal)
      .then((s) => {
        if (!active) return;
        setSession(s);
        register(s.sessionId, s.name);
      })
      .catch((e) => {
        if (!active || e.name === 'AbortError') return;
        setError(e.message || 'Failed to start stream');
      })
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
      controller.abort();
    };
  }, [magnet, attempt, register]);

  // Unregister when the panel unmounts or the session changes.
  useEffect(() => {
    if (!session) return undefined;
    return () => unregister(session.sessionId);
  }, [session, unregister]);

  return (
    <div className="stream-panel">
      {loading ? <div className="spinner">Fetching torrent info…</div> : null}
      {!loading ? (
        <div className="stream-files">
          {error ? (
            <>
              <ErrorBanner message={error} onDismiss={() => setError('')} />
              <button style={{ width: 'fit-content' }} onClick={() => setAttempt((a) => a + 1)}><RefreshCw size={14} />Retry</button>
            </>
          ) : !session ? (
            <button style={{ width: 'fit-content' }} onClick={() => setAttempt((a) => a + 1)}><RefreshCw size={14} />Retry</button>
          ) : session.files.length === 0 ? (
            <div className="empty">No files in this torrent.</div>
          ) : (
            session.files.map((f) => (
              <FileRow key={`${session.sessionId}-${f.index}`} session={session} file={f} />
            ))
          )}
        </div>
      ) : null}
    </div>
  );
}
