import { motion } from 'framer-motion';
import { staggerItem } from '../motion.js';

// A single TMDB title card (poster + name + year + overview + media-type badge).
// Shared by TitleList (search results grid) and DiscoverRail (horizontal rails).
export default function TitleCard({ title: t, onSelect, selected }) {
  return (
    <motion.button
      className="title-card"
      variants={staggerItem}
      whileHover={{ y: -3 }}
      whileTap={{ scale: 0.99 }}
      onClick={() => onSelect(t)}
      aria-pressed={selected}
    >
      {t.poster_url ? (
        <img src={t.poster_url} alt="" loading="lazy" />
      ) : (
        <img alt="" />
      )}
      <div className="meta">
        <div className="name">
          {t.title} {t.year ? <span className="sub">({t.year})</span> : null}
        </div>
        {t.overview ? (
          <div className="sub">
            {t.overview.slice(0, 120)}
            {t.overview.length > 120 ? '…' : ''}
          </div>
        ) : null}
      </div>
      <span className="badge">{t.media_type}</span>
    </motion.button>
  );
}
