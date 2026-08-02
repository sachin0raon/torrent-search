import { memo, useCallback, useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { X, ChevronDown, ChevronUp, Trash2, PlayCircle, Download, Pause, Play } from 'lucide-react';
import { downloader } from '../api/downloader.js';
import { buildDownloadUrl, playerLinks, baseName, isMediaFile } from '../playerLinks.js';
import { fadeUp, spring, collapsePanel } from '../motion.js';
import { formatSize } from '../formatSize.js';
import CopyButton from './CopyButton.jsx';

const POLL_INTERVAL = 5000;

// qBittorrent's own "no estimate" sentinel (its ETA field reads 8640000 —
// 100 days — when the estimate is unknown/infinite, e.g. a stalled or
// unselected-only torrent) rather than an actual duration.
const ETA_UNKNOWN = 8640000;

function formatEta(seconds) {
  if (seconds == null || seconds < 0 || seconds >= ETA_UNKNOWN) return null;
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

function DownloadFileRow({ hash, file }) {
  const name = baseName(file.name);
  const url = buildDownloadUrl(hash, file.index, name);
  const links = isMediaFile(name) ? playerLinks(url, name) : [];
  const pct = file.size > 0 ? Math.min(100, Math.round((file.downloaded / file.size) * 100)) : 0;
  return (
    <div className="stats-file-row">
      <div className="stats-file-name">{name}</div>
      <div className="stats-progress-bar">
        <div className="stats-progress-fill" style={{ width: `${pct}%` }} />
      </div>
      <div className="stats-file-meta">
        {pct}% &middot; <span className="meta-chunk">{formatSize(file.downloaded)}</span> / <span className="meta-chunk">{formatSize(file.size)}</span>
      </div>
      <div className="player-links">
        {links.map((l) => (
          <a key={l.id} href={l.href} rel="noreferrer">
            <button className="player-btn">
              <PlayCircle size={14} />
              {l.label}
            </button>
          </a>
        ))}
        <a href={`${url}?dl=1`}>
          <button className="player-btn icon-only" title="Download file" aria-label="Download file">
            <Download size={14} />
          </button>
        </a>
        <CopyButton value={url} label="Copy link" className="player-btn icon-only" />
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
const DownloadCard = memo(function DownloadCard({ entry, onDelete, onRefresh }) {
  const { hash, name } = entry;
  const [expanded, setExpanded] = useState(false);
  const [files, setFiles] = useState(null);
  // Torrent-level snapshot from the same expanded-card poll that fetches
  // `files` below — once populated, this (not the `entry` prop from the
  // parent's separate list poll) drives the card's progress bar/chips too,
  // so an expanded card's own numbers never visibly disagree with its file
  // rows just because the two polls' 5s timers aren't phase-aligned.
  const [detail, setDetail] = useState(null);
  const [loadingFiles, setLoadingFiles] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const confirmTimerRef = useRef(null);
  // Spread rather than swap outright: any field detail actually carries
  // (the normal case — Get always returns the full torrent-level shape)
  // wins, but a field it happens to omit falls back to entry's last-known
  // value instead of going undefined.
  const live = expanded && detail ? { ...entry, ...detail } : entry;
  const { state, progress, dlspeed, size, downloaded, eta, selectedFiles, totalFiles } = live;
  const pct = Math.round((progress ?? 0) * 100);
  const isDone = pct >= 100;
  const isPaused = state === 'pausedDL' || state === 'stoppedDL';
  const etaLabel = formatEta(eta);

  // Per-file progress needs its own live poll while expanded — the parent
  // list poll (below) only ever refreshes each torrent's aggregate progress,
  // so without this a season pack's individual file percentages would freeze
  // at whatever they were the moment the card was expanded.
  useEffect(() => {
    if (!expanded) return undefined;
    let active = true;

    async function loadFiles() {
      try {
        const d = await downloader.getDownload(hash);
        if (active) {
          setFiles((d.files || []).filter((f) => f.selected));
          setDetail(d);
        }
      } catch {
        // Transient failure — keep showing the last-known file list/detail
        // rather than blanking it, unless this was the very first load.
        if (active) setFiles((prev) => prev ?? []);
      } finally {
        if (active) setLoadingFiles(false);
      }
    }

    setLoadingFiles(true);
    loadFiles();
    const id = setInterval(loadFiles, POLL_INTERVAL);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [expanded, hash]);

  function toggle() {
    setExpanded((e) => !e);
  }

  async function handlePause() {
    await downloader.pauseDownload(hash);
    await onRefresh();
  }

  async function handleResume() {
    await downloader.resumeDownload(hash);
    await onRefresh();
  }

  useEffect(() => () => clearTimeout(confirmTimerRef.current), []);

  async function handleDelete() {
    if (!confirmDelete) {
      setConfirmDelete(true);
      confirmTimerRef.current = setTimeout(() => setConfirmDelete(false), 3000);
      return;
    }
    clearTimeout(confirmTimerRef.current);
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
      <div className="stats-file-meta">
        {pct}% &middot; <span className="meta-chunk">{formatSize(downloaded)}</span> / <span className="meta-chunk">{formatSize(size)}</span>
        {/* Only shown for packs (>1 file) — a single-file download is never
            ambiguous about "the whole thing," so "1 of 1 files" would just
            be noise. totalFiles is also 0 both before metadata arrives and
            when the count genuinely couldn't be fetched (see DownloadInfo's
            doc comment), so this condition naturally hides it then too. */}
        {totalFiles > 1 ? <> &middot; <span className="meta-chunk">{selectedFiles} of {totalFiles} files</span></> : null}
        {etaLabel ? <> &middot; <span className="meta-chunk">ETA {etaLabel}</span></> : null}
      </div>
      <div className="stats-card-actions">
        {!isDone && (
          isPaused ? (
            <button onClick={handleResume}>
              <Play size={14} />
              Resume
            </button>
          ) : (
            <button onClick={handlePause}>
              <Pause size={14} />
              Pause
            </button>
          )
        )}
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
  prev.onRefresh === next.onRefresh &&
  prev.entry.hash === next.entry.hash &&
  prev.entry.name === next.entry.name &&
  prev.entry.state === next.entry.state &&
  prev.entry.progress === next.entry.progress &&
  prev.entry.dlspeed === next.entry.dlspeed &&
  prev.entry.size === next.entry.size &&
  prev.entry.downloaded === next.entry.downloaded &&
  prev.entry.eta === next.entry.eta &&
  prev.entry.selectedFiles === next.entry.selectedFiles &&
  prev.entry.totalFiles === next.entry.totalFiles);

export default function DownloadsModal({ onClose }) {
  const [downloads, setDownloads] = useState(null); // null = still loading
  const [error, setError] = useState(false);
  const activeRef = useRef(true);

  const poll = useCallback(async () => {
    try {
      const list = await downloader.listDownloads();
      if (activeRef.current) {
        setDownloads(list || []);
        setError(false);
      }
    } catch {
      if (activeRef.current) setError(true);
    }
  }, []);

  useEffect(() => {
    activeRef.current = true;
    poll();
    const id = setInterval(poll, POLL_INTERVAL);
    return () => {
      activeRef.current = false;
      clearInterval(id);
    };
  }, [poll]);

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
                  <DownloadCard entry={entry} onDelete={handleDelete} onRefresh={poll} />
                </motion.div>
              ))}
            </AnimatePresence>
          ) : null}
        </div>
      </motion.div>
    </motion.div>
  );
}
