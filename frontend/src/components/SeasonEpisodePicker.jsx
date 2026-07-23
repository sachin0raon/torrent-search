import { useState } from 'react';
import { motion } from 'framer-motion';
import { fadeUp, spring } from '../motion.js';

// TV only. Whole series (no season/episode) -> whole-series torrentio call;
// a chosen season + episode -> that specific episode.
//
// When `seasons` (from TMDB) is available, render cascading Season -> Episode
// dropdowns. Otherwise fall back to manual number inputs.
export default function SeasonEpisodePicker({ onFetch, seasons, loadingSeasons }) {
  const hasSeasons = Array.isArray(seasons) && seasons.length > 0;
  const [season, setSeason] = useState('');
  const [episode, setEpisode] = useState('');

  const selectedSeason = hasSeasons
    ? seasons.find((s) => String(s.season_number) === season)
    : null;
  const episodeCount = selectedSeason ? selectedSeason.episode_count : 0;

  function onSeasonChange(value) {
    setSeason(value);
    // Default to episode 1 when a season is chosen; clear when back to whole series.
    setEpisode(value === '' ? '' : '1');
  }

  // Manual fallback needs both-or-neither; dropdown mode is always valid.
  const bothOrNeither =
    hasSeasons || (season === '' && episode === '') || (season !== '' && episode !== '');

  function submit(e) {
    e.preventDefault();
    if (season === '') {
      onFetch({});
    } else {
      onFetch({ season: Number(season), episode: Number(episode) });
    }
  }

  if (loadingSeasons) {
    return <div className="spinner">Loading seasons…</div>;
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
      {hasSeasons ? (
        <>
          <div className="field">
            <label htmlFor="season">Season</label>
            <select id="season" value={season} onChange={(e) => onSeasonChange(e.target.value)}>
              <option value="">Whole series</option>
              {seasons.map((s) => (
                <option key={s.season_number} value={s.season_number}>
                  {s.season_number === 0 ? 'Specials' : `Season ${s.season_number}`} ({s.episode_count} ep)
                </option>
              ))}
            </select>
          </div>
          {season !== '' ? (
            <div className="field">
              <label htmlFor="episode">Episode</label>
              <select id="episode" value={episode} onChange={(e) => setEpisode(e.target.value)}>
                {Array.from({ length: episodeCount }, (_, i) => i + 1).map((n) => (
                  <option key={n} value={n}>
                    Episode {n}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
        </>
      ) : (
        <>
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
        </>
      )}

      <button className="primary" type="submit" disabled={!bothOrNeither}>
        Find torrents
      </button>
      {!hasSeasons && !bothOrNeither ? (
        <span className="notice">Set both season and episode, or leave both empty for the whole series.</span>
      ) : null}
    </motion.form>
  );
}
