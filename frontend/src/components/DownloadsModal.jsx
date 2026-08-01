import { memo, useCallback, useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { X, ChevronDown, ChevronUp, Trash2 } from 'lucide-react';
import { downloader } from '../api/downloader.js';
import { buildDownloadUrl, playerLinks, baseName } from '../playerLinks.js';
import { fadeUp, spring, collapsePanel } from '../motion.js';
import { formatSize } from '../formatSize.js';
import CopyButton from './CopyButton.jsx';

const POLL_INTERVAL = 5000;

function DownloadFileRow({ hash, file }) {
  const name = baseName(file.name);
  const url = buildDownloadUrl(hash, file.index, name);
  const links = playerLinks(url, name);
  const pct = file.size > 0 ? Math.min(100, Math.round((file.downloaded / file.size) * 100)) : 0;
  return (
    <div className="stats-file-row">
      <div className="stats-file-name">{name}</div>
      <div className="stats-progress-bar">
        <div className="stats-progress-fill" style={{ width: `${pct}%` }} />
      </div>
      <div className="stats-file-meta">
        {pct}% &middot; {formatSize(file.downloaded)} / {formatSize(file.size)}
      </div>
      <div className="player-links">
        {links.map((l) => (
          <a key={l.id} href={l.href} rel="noreferrer">
            <button className="player-btn">{l.label}</button>
          </a>
        ))}
        <a href={`${url}?dl=1`}>
          <button className="player-btn">Download file</button>
        </a>
        <CopyButton value={url} label="Copy link" className="" />
      </div>
    </div>
  );
}

// Memoized with a field-level comparator (not React.memo's default shallow-
// prop-equality) because `entry` is a brand-new object every 5s poll tick
// even when nothing about this particular torrent changed — a plain identity
// check would never skip a re-render. Requires `onDelete` to be a stable
// reference too (see DownloadsModal's useCallback below), or the comparator
// would still see a "changed" prop on every poll.
const DownloadCard = memo(function DownloadCard({ entry, onDelete }) {
  const { hash, name, state, progress, dlspeed } = entry;
  const [expanded, setExpanded] = useState(false);
  const [files, setFiles] = useState(null);
  const [loadingFiles, setLoadingFiles] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const pct = Math.round((progress ?? 0) * 100);

  async function toggle() {
    const next = !expanded;
    setExpanded(next);
    if (next && files === null && !loadingFiles) {
      setLoadingFiles(true);
      try {
        const detail = await downloader.getDownload(hash);
        setFiles((detail.files || []).filter((f) => f.selected));
      } catch {
        setFiles([]);
      } finally {
        setLoadingFiles(false);
      }
    }
  }

  async function handleDelete() {
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    setDeleting(true);
    try {
      await onDelete(hash);
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div className="stats-session-card">
      <div className="stats-session-header">
        <span className="stats-session-name">{name}</span>
        <div className="stats-session-chips">
          <span className="stats-chip">{state}</span>
          {dlspeed > 0 ? <span className="stats-chip">{formatSize(dlspeed)}/s</span> : null}
        </div>
      </div>
      <div className="stats-progress-bar">
        <div className="stats-progress-fill" style={{ width: `${pct}%` }} />
      </div>
      <div className="stats-file-meta">{pct}%</div>
      <div className="stats-card-actions">
        <button onClick={toggle} aria-expanded={expanded}>
          {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
          {expanded ? 'Hide files' : 'Show files'}
        </button>
        <button onClick={handleDelete} disabled={deleting}>
          <Trash2 size={14} />
          {confirmDelete ? 'Confirm delete?' : 'Delete'}
        </button>
      </div>
      <AnimatePresence initial={false}>
        {expanded ? (
          <motion.div key="files" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <div className="stats-file-list" style={{ marginTop: 10 }}>
              {loadingFiles ? <div className="spinner">Loading files…</div> : null}
              {files && files.length === 0 && !loadingFiles ? (
                <div className="stats-empty-files">No files selected yet.</div>
              ) : null}
              {files ? files.map((f) => <DownloadFileRow key={f.index} hash={hash} file={f} />) : null}
            </div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}, (prev, next) =>
  prev.onDelete === next.onDelete &&
  prev.entry.hash === next.entry.hash &&
  prev.entry.name === next.entry.name &&
  prev.entry.state === next.entry.state &&
  prev.entry.progress === next.entry.progress &&
  prev.entry.dlspeed === next.entry.dlspeed &&
  prev.entry.size === next.entry.size);

export default function DownloadsModal({ onClose }) {
  const [downloads, setDownloads] = useState(null); // null = still loading
  const [error, setError] = useState(false);
  const activeRef = useRef(true);

  useEffect(() => {
    activeRef.current = true;

    async function poll() {
      try {
        const list = await downloader.listDownloads();
        if (activeRef.current) {
          setDownloads(list || []);
          setError(false);
        }
      } catch {
        if (activeRef.current) setError(true);
      }
    }

    poll();
    const id = setInterval(poll, POLL_INTERVAL);
    return () => {
      activeRef.current = false;
      clearInterval(id);
    };
  }, []);

  // useCallback so DownloadCard's memo comparator (above) sees a stable
  // onDelete reference across polls — otherwise every poll's re-render of
  // DownloadsModal would hand every card a "new" callback, defeating the
  // field-level comparator entirely.
  const handleDelete = useCallback(async (hash) => {
    await downloader.deleteDownload(hash);
    setDownloads((prev) => (prev ? prev.filter((d) => d.hash !== hash) : prev));
  }, []);

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
        aria-label="Downloads"
      >
        <div className="modal-header">
          <h2 className="modal-title">Downloads</h2>
          <button className="icon-btn" onClick={onClose} aria-label="Close">
            <X size={18} />
          </button>
        </div>

        <div className="stats-modal-body">
          {downloads === null && !error ? <div className="spinner">Loading downloads…</div> : null}
          {error ? <div className="empty">Couldn't reach the download manager.</div> : null}
          {downloads && downloads.length === 0 ? (
            <div className="empty">No downloads yet.</div>
          ) : null}
          {downloads ? (
            <AnimatePresence initial={false}>
              {downloads.map((entry) => (
                <motion.div key={entry.hash} variants={fadeUp} initial="initial" animate="animate" exit="exit" transition={spring}>
                  <DownloadCard entry={entry} onDelete={handleDelete} />
                </motion.div>
              ))}
            </AnimatePresence>
          ) : null}
        </div>
      </motion.div>
    </motion.div>
  );
}
