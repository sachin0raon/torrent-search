import { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { Zap, Download } from 'lucide-react';
import { api } from '../api/client.js';
import CopyButton from './CopyButton.jsx';
import StreamPanel from './StreamPanel.jsx';
import DownloadPanel from './DownloadPanel.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import { useDownloadsEnabled } from '../downloadCapabilityContext.jsx';
import { staggerItem, collapsePanel } from '../motion.js';

export default function X1337Row({ item }) {
  const [streamOpen, setStreamOpen] = useState(false);
  const [downloadOpen, setDownloadOpen] = useState(false);
  const [magnet, setMagnet] = useState(null);
  const [loadingMagnet, setLoadingMagnet] = useState(false);
  const [error, setError] = useState('');
  const downloadsEnabled = useDownloadsEnabled();

  async function ensureMagnet() {
    if (magnet) return magnet;
    setLoadingMagnet(true);
    setError('');
    try {
      const data = await api.x1337Magnet({ path: item.detail_path });
      if (data && data.magnet) {
        setMagnet(data.magnet);
        return data.magnet;
      }
      throw new Error('Magnet link not found');
    } catch (e) {
      setError(e.message || 'Failed to fetch magnet link');
      return null;
    } finally {
      setLoadingMagnet(false);
    }
  }

  async function toggleStream() {
    if (streamOpen) {
      setStreamOpen(false);
      return;
    }
    const m = await ensureMagnet();
    if (m) {
      setDownloadOpen(false);
      setStreamOpen(true);
    }
  }

  async function toggleDownload() {
    if (downloadOpen) {
      setDownloadOpen(false);
      return;
    }
    const m = await ensureMagnet();
    if (m) {
      setStreamOpen(false);
      setDownloadOpen(true);
    }
  }

  return (
    <motion.div className="result-row" variants={staggerItem} whileHover={{ y: -2 }}>
      <div className="row-main">
        <div style={{ minWidth: 0 }}>
          <div className="name">{item.title}</div>
          <div className="stats">
            <span>👤 {item.seeds ?? '—'}</span>
            <span>🔴 {item.leeches ?? '—'}</span>
            {item.size ? <span>💾 {item.size}</span> : null}
            {item.date ? <span>📅 {item.date}</span> : null}
          </div>
        </div>
        <div className="actions">
          <button onClick={toggleStream} disabled={loadingMagnet}>
            <Zap size={13} />
            {loadingMagnet ? 'Fetching…' : streamOpen ? 'Hide stream' : 'Stream'}
          </button>
          {downloadsEnabled ? (
            <button onClick={toggleDownload} disabled={loadingMagnet}>
              <Download size={13} />
              {loadingMagnet ? 'Fetching…' : downloadOpen ? 'Hide download' : 'Download'}
            </button>
          ) : null}
          <CopyButton value={magnet} getValue={ensureMagnet} className="action-copy" />
        </div>
      </div>
      <ErrorBanner message={error} onDismiss={() => setError('')} />
      <AnimatePresence initial={false}>
        {streamOpen && magnet && (
          <motion.div key="stream" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <StreamPanel magnet={magnet} />
          </motion.div>
        )}
        {downloadOpen && magnet && (
          <motion.div key="download" {...collapsePanel} style={{ overflow: 'hidden' }}>
            <DownloadPanel magnet={magnet} />
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
