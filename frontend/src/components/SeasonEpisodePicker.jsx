import { useState } from 'react';
import { motion } from 'framer-motion';
import { fadeUp, spring } from '../motion.js';

// TV only. Empty season/episode -> whole-series torrentio call;
// both filled -> specific episode.
export default function SeasonEpisodePicker({ onFetch }) {
  const [season, setSeason] = useState('');
  const [episode, setEpisode] = useState('');

  const bothOrNeither =
    (season === '' && episode === '') || (season !== '' && episode !== '');

  function submit(e) {
    e.preventDefault();
    onFetch({
      season: season === '' ? undefined : Number(season),
      episode: episode === '' ? undefined : Number(episode),
    });
  }

  return (
    <motion.form
      className="season-episode"
      onSubmit={submit}
      variants={fadeUp}
      initial="initial"
      animate="animate"
      transition={spring}
    >
      <div className="field">
        <label htmlFor="season">Season (optional)</label>
        <input
          id="season"
          type="number"
          min="1"
          value={season}
          onChange={(e) => setSeason(e.target.value)}
          placeholder="—"
        />
      </div>
      <div className="field">
        <label htmlFor="episode">Episode (optional)</label>
        <input
          id="episode"
          type="number"
          min="1"
          value={episode}
          onChange={(e) => setEpisode(e.target.value)}
          placeholder="—"
        />
      </div>
      <button className="primary" type="submit" disabled={!bothOrNeither}>
        Find torrents
      </button>
      {!bothOrNeither ? (
        <span className="notice">Set both season and episode, or leave both empty for the whole series.</span>
      ) : null}
    </motion.form>
  );
}
