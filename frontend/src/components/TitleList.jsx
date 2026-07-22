import { motion } from 'framer-motion';
import { staggerContainer, staggerItem } from '../motion.js';

// Stage 1 results: TMDB titles. Clicking a card advances to stream lookup.
export default function TitleList({ results, onSelect, selectedId }) {
  if (!results.length) {
    return <div className="empty">No titles found.</div>;
  }
  return (
    <motion.div
      className="title-list"
      variants={staggerContainer}
      initial="initial"
      animate="animate"
    >
      {results.map((t) => (
        <motion.button
          key={`${t.media_type}-${t.tmdb_id}`}
          className="title-card"
          variants={staggerItem}
          whileHover={{ y: -3 }}
          whileTap={{ scale: 0.99 }}
          onClick={() => onSelect(t)}
          aria-pressed={selectedId === `${t.media_type}-${t.tmdb_id}`}
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
      ))}
    </motion.div>
  );
}
