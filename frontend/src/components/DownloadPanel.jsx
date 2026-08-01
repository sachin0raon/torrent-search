import { useEffect, useState } from 'react';
import { RefreshCw, Download } from 'lucide-react';
import { downloader } from '../api/downloader.js';
import ErrorBanner from './ErrorBanner.jsx';
import { formatSize } from '../formatSize.js';
import { baseName } from '../playerLinks.js';

// Inline download panel — mirrors StreamPanel.jsx's structure (fetch on
// mount, spinner -> file list, Retry on failure), but the file list is
// multi-select (checkboxes, not per-row expand) since a download-manager
// selection is additive across files rather than one-file-at-a-time.
// Rendered directly inside result rows, no modal.
export default function DownloadPanel({ magnet }) {
  const [torrent, setTorrent] = useState(null); // { hash, name, files }
  const [selected, setSelected] = useState(() => new Set());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [starting, setStarting] = useState(false);
  const [started, setStarted] = useState(false);

  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    setLoading(true);
    setError('');
    setStarted(false);
    downloader
      .createDownload(magnet, controller.signal)
      .then((t) => {
        if (!active) return;
        setTorrent(t);
        setSelected(new Set());
      })
      .catch((e) => {
        if (!active || e.name === 'AbortError') return;
        setError(e.message || 'Failed to add download');
      })
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
      controller.abort();
    };
  }, [magnet, attempt]);

  function toggleFile(index) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  }

  function toggleSelectAll() {
    setSelected((prev) =>
      prev.size === torrent.files.length ? new Set() : new Set(torrent.files.map((f) => f.index)),
    );
  }

  async function startDownload() {
    if (!torrent || selected.size === 0) return;
    setStarting(true);
    setError('');
    try {
      await downloader.selectFiles(torrent.hash, Array.from(selected));
      setStarted(true);
    } catch (e) {
      setError(e.message || 'Failed to start download');
    } finally {
      setStarting(false);
    }
  }

  return (
    <div className="stream-panel">
      {loading ? <div className="spinner">Fetching torrent info…</div> : null}
      {!loading ? (
        <div className="stream-files">
          {error ? (
            <>
              <ErrorBanner message={error} onDismiss={() => setError('')} />
              <button style={{ width: 'fit-content' }} onClick={() => setAttempt((a) => a + 1)}>
                <RefreshCw size={14} />Retry
              </button>
            </>
          ) : !torrent ? (
            <button style={{ width: 'fit-content' }} onClick={() => setAttempt((a) => a + 1)}>
              <RefreshCw size={14} />Retry
            </button>
          ) : torrent.files.length === 0 ? (
            <div className="empty">No files in this torrent.</div>
          ) : started ? (
            <div className="notice">Downloading — check the Downloads list for progress.</div>
          ) : (
            <>
              {torrent.files.length > 1 ? (
                <label className="stream-file-row download-file-row download-select-all">
                  <input
                    type="checkbox"
                    checked={selected.size === torrent.files.length}
                    ref={(el) => {
                      if (el) el.indeterminate = selected.size > 0 && selected.size < torrent.files.length;
                    }}
                    onChange={toggleSelectAll}
                  />
                  <span className="fname">
                    {selected.size === torrent.files.length ? 'Deselect all' : 'Select all'}
                  </span>
                </label>
              ) : null}
              {torrent.files.map((f) => (
                <label key={f.index} className="stream-file-row download-file-row">
                  <input
                    type="checkbox"
                    checked={selected.has(f.index)}
                    onChange={() => toggleFile(f.index)}
                  />
                  <span className="fname">{baseName(f.name)}</span>
                  <span className="notice">{formatSize(f.size)}</span>
                </label>
              ))}
              {/* Sticky, not just appended after the list: .stream-files scrolls
                  internally (max-height: 60vh) for a large season pack, so a
                  plain trailing button would sit below the fold, out of reach
                  without scrolling all the way down first. */}
              <div className="download-start-bar">
                <button
                  style={{ width: 'fit-content' }}
                  disabled={selected.size === 0 || starting}
                  onClick={startDownload}
                >
                  <Download size={14} />
                  {starting ? 'Starting…' : `Download ${selected.size || ''} file${selected.size === 1 ? '' : 's'}`.trim()}
                </button>
              </div>
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}
