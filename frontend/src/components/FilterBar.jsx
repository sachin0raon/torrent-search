import { RESOLUTION_OPTIONS, LANGUAGE_OPTIONS } from '../filterState.js';

export default function FilterBar({ resolution, language, onResolutionChange, onLanguageChange }) {
  return (
    <div className="filter-bar">
      <div className="filter-field">
        <label htmlFor="filter-resolution">Resolution</label>
        <select
          id="filter-resolution"
          value={resolution}
          onChange={(e) => onResolutionChange(e.target.value)}
        >
          {RESOLUTION_OPTIONS.map((o) => (
            <option key={o} value={o}>{o}</option>
          ))}
        </select>
      </div>
      <div className="filter-field">
        <label htmlFor="filter-language">Language</label>
        <select
          id="filter-language"
          value={language}
          onChange={(e) => onLanguageChange(e.target.value)}
        >
          {LANGUAGE_OPTIONS.map((o) => (
            <option key={o} value={o}>{o}</option>
          ))}
        </select>
      </div>
    </div>
  );
}
