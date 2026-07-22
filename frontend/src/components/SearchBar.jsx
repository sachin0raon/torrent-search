import { useState } from 'react';

export default function SearchBar({ onSearch, disabled }) {
  const [value, setValue] = useState('');

  function submit(e) {
    e.preventDefault();
    const q = value.trim();
    if (q) onSearch(q);
  }

  return (
    <form className="search-bar" onSubmit={submit}>
      <input
        type="text"
        placeholder="Search movies or TV shows…"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        aria-label="Search query"
        autoFocus
      />
      <button className="primary btn-icon" type="submit" disabled={disabled || !value.trim()}>
        Search
        <span className="icon-circle" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
            strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 12h14M13 6l6 6-6 6" />
          </svg>
        </span>
      </button>
    </form>
  );
}
