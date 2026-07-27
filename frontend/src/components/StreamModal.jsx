import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { streamer } from '../api/streamer.js';
import ErrorBanner from './ErrorBanner.jsx';
import CopyButton from './CopyButton.jsx';
import { buildStreamUrl, playerLinks, baseName } from '../playerLinks.js';
import { spring } from '../motion.js';

function formatSize(bytes) {
  if (!bytes || bytes < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

// One file row: for streamable files, offers player deep links + copy stream URL.
function FileRow({ session, file }) {
  if (!file.streamable) {
    return (
      <div className="forum-link-row">
        <span className="fname">{baseName(file.path)}</span>
        <div className="actions">
          <span className="notice">{formatSize(file.size)}</span>
          <span className="notice">not playable</span>
        </div>
      </div>
    );
  }

  const name = baseName(file.path);
  const url = buildStreamUrl(session.sessionId, file.index, name);
  const links = playerLinks(url, name);

  return (
    <div className="stream-file-row">
      <div className="stream-file-head">
        <span className="fname">{name}</span>
        <span className="notice">{formatSize(file.size)}</span>
      </div>
      <div className="player-links">
        {links.map((l) => (
          <a key={l.id} href={l.href} rel="noreferrer">
            <button className="player-btn">{l.label}</button>
          </a>
        ))}
        <CopyButton value={url} label="Copy stream URL" className="" />
      </div>
    </div>
  );
}

// Adds a magnet to the streaming service, then lists its files with playback
// links. Rendered inside an AnimatePresence by StreamButton.
export default function StreamModal({ magnet, onClose }) {
  const [session, setSession] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    setLoading(true);
    setError('');
    streamer
      .createSession(magnet, controller.signal)
      .then((s) => active && setSession(s))
      .catch((e) => {
        if (!active || e.name === 'AbortError') return;
        setError(e.message || 'Failed to start stream');
      })
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
      controller.abort();
    };
  }, [magnet, attempt]);

  const streamable = session ? session.files.filter((f) => f.streamable) : [];

  return (
    <motion.div
      className="modal-backdrop"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.25 }}
    >
      <motion.div
        className="modal"
        onMouseDown={(e) => e.stopPropagation()}
        initial={{ opacity: 0, y: 24, scale: 0.96 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 16, scale: 0.97 }}
        transition={spring}
      >
        <h2>Stream{session?.name ? `: ${session.name}` : ''}</h2>

        {loading ? <div className="spinner">Fetching torrent info…</div> : null}

        {error ? (
          <div>
            <ErrorBanner message={error} onDismiss={() => setError('')} />
            <button onClick={() => setAttempt((a) => a + 1)}>Retry</button>
          </div>
        ) : null}

        {session && !loading ? (
          <div className="stream-files">
            {session.files.length === 0 ? (
              <div className="empty">No files in this torrent.</div>
            ) : streamable.length === 0 ? (
              <div className="empty">No playable video files in this torrent.</div>
            ) : (
              session.files.map((f) => (
                <FileRow key={f.index} session={session} file={f} />
              ))
            )}
          </div>
        ) : null}

        <div className="actions">
          <button onClick={onClose}>Close</button>
        </div>
      </motion.div>
    </motion.div>
  );
}
