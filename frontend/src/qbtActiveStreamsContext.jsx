import { createContext, useContext, useEffect, useState } from 'react';
import { streamer } from './api/streamer.js';
import { useAuth } from './auth/AuthContext.jsx';

// Whether the qBittorrent-engine-only Active Streams panel (docs/STREAMING.md
// §7 — pause/resume/retain, Move to Downloads, Flush) is available. Checked
// once by attempting GET /stream-api/torrents: it 404s when the active engine
// doesn't implement it (i.e. STREAM_ENGINE=anacrolix), the same
// route-absent-means-disabled convention downloadCapabilityContext.jsx
// already uses for /download-api/status. A context, not a prop, for the same
// reason as that one — the FAB in App.jsx needs this before the modal ever
// opens.
const QbtActiveStreamsContext = createContext(false);

export function QbtActiveStreamsProvider({ children }) {
  const [enabled, setEnabled] = useState(false);
  // /stream-api/torrents is behind the session gate. Refetch on account change
  // so login/logout flips the Active Streams panel instead of latching the
  // logged-out 401.
  const { available, ready, user } = useAuth();

  useEffect(() => {
    if (available && (!ready || !user)) {
      setEnabled(false);
      return undefined;
    }

    let active = true;
    streamer
      .listActiveTorrents()
      .then(() => {
        if (active) setEnabled(true);
      })
      .catch(() => {
        // 404 (anacrolix engine) or backend unreachable — stays false, the
        // existing local-session-only Active Streams behavior is unaffected.
        if (active) setEnabled(false);
      });
    return () => {
      active = false;
    };
  }, [available, ready, user]);

  return (
    <QbtActiveStreamsContext.Provider value={enabled}>
      {children}
    </QbtActiveStreamsContext.Provider>
  );
}

export function useQbtActiveStreamsEnabled() {
  return useContext(QbtActiveStreamsContext);
}
