import { useState } from 'react';
import { AnimatePresence } from 'framer-motion';
import StreamModal from './StreamModal.jsx';

// Opens a StreamModal for a magnet. Used on any result row that carries a
// magnet (Torrentio items and expanded Forum topic links).
export default function StreamButton({ magnet, title, className = '' }) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button className={className} onClick={() => setOpen(true)} disabled={!magnet}>
        Stream
      </button>
      <AnimatePresence>
        {open ? (
          <StreamModal magnet={magnet} title={title} onClose={() => setOpen(false)} />
        ) : null}
      </AnimatePresence>
    </>
  );
}
