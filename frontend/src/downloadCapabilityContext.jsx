import { createContext, useContext, useEffect, useState } from 'react';
import { downloader } from './api/downloader.js';

// Whether the persistent download-manager feature (docs/STREAMING.md §6) is
// available — checked once against /download-api/status. A context (rather
// than a prop) so the "Download" toggle deep inside ResultTabs/TorrentRow/
// ForumTopicRow, and the header button that opens DownloadsModal, don't need
// it threaded through every intermediate component — same rationale as
// sessionContext.jsx's SessionProvider for StatsModal's FAB visibility.
const DownloadCapabilityContext = createContext(false);

export function DownloadCapabilityProvider({ children }) {
  const [enabled, setEnabled] = useState(false);

  useEffect(() => {
    let active = true;
    downloader
      .getStatus()
      .then((s) => {
        if (active) setEnabled(!!s?.enabled);
      })
      .catch(() => {
        // Feature absent or backend unreachable — stays hidden, not an error
        // worth surfacing to the user.
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <DownloadCapabilityContext.Provider value={enabled}>
      {children}
    </DownloadCapabilityContext.Provider>
  );
}

export function useDownloadsEnabled() {
  return useContext(DownloadCapabilityContext);
}
