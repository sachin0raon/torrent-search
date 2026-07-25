import { motion } from 'framer-motion';
import { staggerContainer } from '../motion.js';
import TitleCard from './TitleCard.jsx';

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
        <TitleCard
          key={`${t.media_type}-${t.tmdb_id}`}
          title={t}
          onSelect={onSelect}
          selected={selectedId === `${t.media_type}-${t.tmdb_id}`}
        />
      ))}
    </motion.div>
  );
}
