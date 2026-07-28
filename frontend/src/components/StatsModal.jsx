import { useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { X } from 'lucide-react';
import { streamer } from '../api/streamer.js';
import { useSessions } from '../sessionContext.jsx';
import { fadeUp, spring } from '../motion.js';
import { formatSize } from '../formatSize.js';

const POLL_INTERVAL = 5000;

function FileProgressRow({ file }) {
  const pct = file.size > 0
    ? Math.min(100, Math.round((file.downloaded / file.size) * 100))
    : 0;
  return (
    <div className="stats-file-row">
      <div className="stats-file-name">{file.name}</div>
      <div className="stats-progress-bar">
        <div className="stats-progress-fill" style={{ width: `${pct}%` }} />
      </div>
      <div className="stats-file-meta">
        {pct}% &middot; {formatSize(file.downloaded)} / {formatSize(file.size)}
      </div>
    </div>
  );
}

function SessionCard({ entry }) {
  const { name, seeders, files, expired, error } = entry;
  return (
    <div className={`stats-session-card${expired ? ' stats-session-expired' : ''}`}>
      <div className="stats-session-header">
        <span className="stats-session-name">{name}</span>
        <div className="stats-session-chips">
          {!expired && (
            <span className="stats-chip">{seeders} seed{seeders !== 1 ? 's' : ''}</span>
          )}
          {expired && <span className="stats-chip stats-chip-expired">Session expired</span>}
          {error && !expired && <span className="stats-chip stats-chip-error" title="Last poll failed">⚠</span>}
        </div>
      </div>
      {!expired && files && files.length > 0 && (
        <div className="stats-file-list">
          {files.map((f) => <FileProgressRow key={f.index} file={f} />)}
        </div>
      )}
      {!expired && files && files.length === 0 && (
        <div className="stats-empty-files">No files</div>
      )}
      {!expired && !files && (
        <div className="spinner" style={{ margin: '8px 0' }}>Loading…</div>
      )}
    </div>
  );
}

export default function StatsModal({ onClose }) {
  const { sessions } = useSessions();
  // Map<sessionId, { name, seeders, files, expired, error }>
  const [statsMap, setStatsMap] = useState(() => {
    const m = new Map();
    sessions.forEach(({ sessionId, name }) => m.set(sessionId, { name, seeders: 0, files: null, expired: false, error: false }));
    return m;
  });
  const sessionsRef = useRef(sessions);
  sessionsRef.current = sessions;

  useEffect(() => {
    let active = true;

    async function poll() {
      const current = sessionsRef.current;
      await Promise.all(
        Array.from(current.values()).map(async ({ sessionId, name }) => {
          try {
            const data = await streamer.getStats(sessionId);
            if (!active) return;
            setStatsMap((prev) => {
              const next = new Map(prev);
              next.set(sessionId, { name: data.name || name, seeders: data.seeders ?? 0, files: data.files ?? [], expired: false, error: false });
              return next;
            });
          } catch (e) {
            if (!active) return;
            const expired = e.status === 410;
            setStatsMap((prev) => {
              const next = new Map(prev);
              const existing = prev.get(sessionId) ?? { name, seeders: 0, files: null };
              next.set(sessionId, { ...existing, expired, error: !expired });
              return next;
            });
          }
        })
      );
    }

    poll();
    const id = setInterval(poll, POLL_INTERVAL);
    return () => { active = false; clearInterval(id); };
  }, []);

  // Sync new sessions that appear while modal is open.
  useEffect(() => {
    sessions.forEach(({ sessionId, name }) => {
      setStatsMap((prev) => {
        if (prev.has(sessionId)) return prev;
        const next = new Map(prev);
        next.set(sessionId, { name, seeders: 0, files: null, expired: false, error: false });
        return next;
      });
    });
  }, [sessions]);

  const entries = Array.from(statsMap.entries()); // [sessionId, entry]

  return (
    <motion.div
      className="modal-backdrop"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      onClick={(e) => e.target === e.currentTarget && onClose()}
    >
      <motion.div
        className="modal stats-modal"
        variants={fadeUp}
        initial="initial"
        animate="animate"
        exit="exit"
        transition={spring}
        role="dialog"
        aria-modal="true"
        aria-label="Active streams"
      >
        <div className="modal-header">
          <h2 className="modal-title">Active Streams</h2>
          <button className="icon-btn" onClick={onClose} aria-label="Close">
            <X size={18} />
          </button>
        </div>

        <div className="stats-modal-body">
          {entries.length === 0 ? (
            <div className="empty">No active streams.</div>
          ) : (
            <AnimatePresence initial={false}>
              {entries.map(([sessionId, entry]) => (
                <motion.div key={sessionId} variants={fadeUp} initial="initial" animate="animate" exit="exit" transition={spring}>
                  <SessionCard entry={entry} />
                </motion.div>
              ))}
            </AnimatePresence>
          )}
        </div>
      </motion.div>
    </motion.div>
  );
}
