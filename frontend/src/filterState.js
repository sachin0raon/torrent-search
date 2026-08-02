import { useState } from 'react';

export const RESOLUTION_OPTIONS = ['All', '4K', '1080p', '720p'];
export const LANGUAGE_OPTIONS = ['All', 'Hindi', 'Tamil', 'Telugu'];

const DEFAULTS = { resolution: '1080p', language: 'All' };
const LS_RESOLUTION = 'filterResolution';
const LS_LANGUAGE = 'filterLanguage';

function readFromStorage() {
  try {
    const resolution = localStorage.getItem(LS_RESOLUTION);
    const language = localStorage.getItem(LS_LANGUAGE);
    return {
      resolution: RESOLUTION_OPTIONS.includes(resolution) ? resolution : DEFAULTS.resolution,
      language: LANGUAGE_OPTIONS.includes(language) ? language : DEFAULTS.language,
    };
  } catch {
    return { ...DEFAULTS };
  }
}

function matchesResolution(title, resolution) {
  if (resolution === 'All') return true;
  const t = title || '';
  if (resolution === '4K') return /4k|2160p|UHD/i.test(t);
  return t.toLowerCase().includes(resolution.toLowerCase());
}

const LANGUAGE_ALIASES = {
  Hindi:   ['hindi', 'hin', 'hi'],
  Tamil:   ['tamil', 'tam'],
  Telugu:  ['telugu', 'tel'],
};

function matchesLanguage(title, language) {
  if (language === 'All') return true;
  const t = (title || '').toLowerCase();
  if (t.includes('multi') || t.includes('dual')) return true;
  const aliases = LANGUAGE_ALIASES[language] ?? [language.toLowerCase()];
  return aliases.some((a) => t.includes(a));
}

export function applyFilters(items, { resolution, language }) {
  if (resolution === 'All' && language === 'All') return items;
  return items.filter(
    (item) => matchesResolution(item.title, resolution) && matchesLanguage(item.title, language),
  );
}

export function useFilterState() {
  const [state, setState] = useState(readFromStorage);

  function setResolution(resolution) {
    try { localStorage.setItem(LS_RESOLUTION, resolution); } catch {}
    setState((s) => ({ ...s, resolution }));
  }

  function setLanguage(language) {
    try { localStorage.setItem(LS_LANGUAGE, language); } catch {}
    setState((s) => ({ ...s, language }));
  }

  return { ...state, setResolution, setLanguage };
}
