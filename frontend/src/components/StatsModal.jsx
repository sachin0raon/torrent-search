import { memo, useCallback, useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { X, Play, HardDriveDownload, Trash2 } from 'lucide-react';
import { streamer } from '../api/streamer.js';
import { useSessions } from '../sessionContext.jsx';
import { useQbtActiveStreamsEnabled } from '../qbtActiveStreamsContext.jsx';
import { useDownloadsEnabled } from '../downloadCapabilityContext.jsx';
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

// --- docs/STREAMING.md §7: qBittorrent-engine-only Active Streams view ---

// Memoized with a field-level comparator (not React.memo's default shallow-
// prop-equality), mirroring DownloadsModal.jsx's DownloadCard — `torrent` is
// a brand-new object every 5s poll tick (a fresh JSON-parsed array from
// listActiveTorrents) even when nothing about this particular torrent
// changed, so a plain identity check would never skip a re-render. Requires
// `onAction` to be a stable reference too (see QbtActiveStreamsView's
// useCallback below), or the comparator would still see a "changed" prop on
// every poll.
const QbtTorrentCard = memo(function QbtTorrentCard({ torrent, downloadsEnabled, onAction, busy }) {
  const { hash, name, progress, size, downloaded, paused } = torrent;
  const pct = Math.min(100, Math.round((progress || 0) * 100));
  return (
    <div className="stats-session-card">
      <div className="stats-session-header">
        <span className="stats-session-name">{name}</span>
        <div className="stats-session-chips">
          {paused && <span className="stats-chip">Paused</span>}
        </div>
      </div>
      <div className="stats-file-list">
        <div className="stats-file-row">
          <div className="stats-progress-bar">
            <div className="stats-progress-fill" style={{ width: `${pct}%` }} />
          </div>
          <div className="stats-file-meta">
            {pct}% &middot; {formatSize(downloaded)} / {formatSize(size)}
          </div>
        </div>
      </div>
      <div className="stats-card-actions">
        {paused && (
          <button disabled={busy} onClick={() => onAction(hash, 'resume')}>
            <Play size={14} />
            Resume
          </button>
        )}
        {downloadsEnabled && (
          <button disabled={busy} onClick={() => onAction(hash, 'move')}>
            <HardDriveDownload size={14} />
            Move to Downloads
          </button>
        )}
        <button disabled={busy} onClick={() => onAction(hash, 'delete')}>
          <Trash2 size={14} />
          Delete
        </button>
      </div>
    </div>
  );
}, (prev, next) =>
  prev.onAction === next.onAction &&
  prev.downloadsEnabled === next.downloadsEnabled &&
  prev.busy === next.busy &&
  prev.torrent.hash === next.torrent.hash &&
  prev.torrent.name === next.torrent.name &&
  prev.torrent.progress === next.torrent.progress &&
  prev.torrent.size === next.torrent.size &&
  prev.torrent.downloaded === next.torrent.downloaded &&
  prev.torrent.paused === next.torrent.paused);

function QbtActiveStreamsView() {
  const downloadsEnabled = useDownloadsEnabled();
  const [torrents, setTorrents] = useState(null); // null = still loading
  const [error, setError] = useState(false);
  const [busyHash, setBusyHash] = useState(null);

  // useCallback (no external dependencies — only stable setState calls and
  // the streamer import) so it's safe to call from the other callbacks below
  // without needing it in their own dependency arrays.
  const refresh = useCallback(async (signal) => {
    try {
      const list = await streamer.listActiveTorrents(signal);
      setTorrents(list || []);
      setError(false);
    } catch (e) {
      if (e.name === 'AbortError') return;
      setError(true);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    refresh(controller.signal);
    const id = setInterval(() => refresh(controller.signal), POLL_INTERVAL);
    return () => {
      controller.abort();
      clearInterval(id);
    };
  }, [refresh]);

  // useCallback so QbtTorrentCard's memo comparator (above) sees a stable
  // reference and can actually skip re-rendering unchanged cards on a poll
  // tick, instead of busting on a fresh handler every render.
  const handleAction = useCallback(async (hash, action) => {
    setBusyHash(hash);
    try {
      if (action === 'resume') await streamer.resumeTorrent(hash);
      else if (action === 'delete') await streamer.deleteTorrent(hash);
      else if (action === 'move') await streamer.moveToDownloads(hash);
      await refresh();
    } catch {
      setError(true);
    } finally {
      setBusyHash(null);
    }
  }, [refresh]);

  const handleFlush = useCallback(async () => {
    setBusyHash('*');
    try {
      await streamer.flushTorrents();
      await refresh();
    } catch {
      setError(true);
    } finally {
      setBusyHash(null);
    }
  }, [refresh]);

  return (
    <>
      <div className="stats-modal-body">
        {torrents === null ? (
          <div className="spinner" style={{ margin: '8px 0' }}>Loading…</div>
        ) : torrents.length === 0 ? (
          <div className="empty">No active streams.</div>
        ) : (
          <AnimatePresence initial={false}>
            {torrents.map((t) => (
              <motion.div key={t.hash} variants={fadeUp} initial="initial" animate="animate" exit="exit" transition={spring}>
                <QbtTorrentCard
                  torrent={t}
                  downloadsEnabled={downloadsEnabled}
                  onAction={handleAction}
                  busy={busyHash === t.hash || busyHash === '*'}
                />
              </motion.div>
            ))}
          </AnimatePresence>
        )}
        {error && <div className="stats-chip stats-chip-error">Couldn't reach the streaming service</div>}
      </div>
      {torrents && torrents.length > 0 && (
        <div className="stats-card-actions">
          <button disabled={busyHash !== null} onClick={handleFlush}>
            <Trash2 size={14} />
            Flush all
          </button>
        </div>
      )}
    </>
  );
}

// --- anacrolix-engine fallback: today's local-session-only view, unchanged ---

function LocalSessionsView() {
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
  );
}

export default function StatsModal({ onClose }) {
  // qBittorrent engine gets a live, clientID-scoped view backed by the
  // Active Streams panel endpoints (docs/STREAMING.md §7); anacrolix keeps
  // today's local-session-only behavior unchanged (§7 Decision #33).
  const qbtActiveStreamsEnabled = useQbtActiveStreamsEnabled();

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

        {qbtActiveStreamsEnabled ? <QbtActiveStreamsView /> : <LocalSessionsView />}
      </motion.div>
    </motion.div>
  );
}
