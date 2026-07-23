import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { api } from '../api/client.js';
import ErrorBanner from './ErrorBanner.jsx';
import { getTorrentioMode, setTorrentioMode } from '../torrentioMode.js';
import { spring } from '../motion.js';

// Edit the configurable forum base URL. Reads current value (env or config)
// and persists an override to the backend config file.
export default function SettingsModal({ onClose, onSaved }) {
  const [value, setValue] = useState('');
  const [source, setSource] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [torrentioMode, setTorrentioModeState] = useState(() => getTorrentioMode());

  // Persist the Torrentio source toggle immediately (it's client-only, so it
  // takes effect on the next search regardless of the forum-URL Save button).
  function onToggleMode(e) {
    const mode = e.target.checked ? 'client' : 'server';
    setTorrentioModeState(mode);
    setTorrentioMode(mode);
  }

  useEffect(() => {
    let active = true;
    api
      .getConfig()
      .then((c) => {
        if (!active) return;
        setValue(c.forum_base_url || '');
        setSource(c.source);
      })
      .catch((e) => active && setError(e.message));
    return () => {
      active = false;
    };
  }, []);

  async function save() {
    setSaving(true);
    setError('');
    try {
      const c = await api.setConfig(value.trim());
      onSaved?.(c);
      onClose();
    } catch (e) {
      setError(e.message || 'Failed to save');
    } finally {
      setSaving(false);
    }
  }

  return (
    <motion.div
      className="modal-backdrop"
      onMouseDown={onClose}
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
        <h2>Forum base URL</h2>
        <div className="row">
          <input
            type="text"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="https://forum.example.tld"
            aria-label="Forum base URL"
          />
          <span className="source-note">
            Current source: {source || 'unknown'}
          </span>
        </div>
        <h2 className="modal-subtitle">Torrentio source</h2>
        <label className="toggle-row">
          <input
            type="checkbox"
            checked={torrentioMode === 'client'}
            onChange={onToggleMode}
          />
          <span>Fetch Torrentio directly in my browser</span>
        </label>
        <span className="source-note">
          {torrentioMode === 'client'
            ? 'Client-side: uses your home IP. Best when the server is blocked (403).'
            : 'Server-side: the backend fetches Torrentio.'}
        </span>

        <ErrorBanner message={error} onDismiss={() => setError('')} />
        <div className="actions">
          <button onClick={onClose}>Cancel</button>
          <button className="primary" onClick={save} disabled={saving || !value.trim()}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </motion.div>
    </motion.div>
  );
}
