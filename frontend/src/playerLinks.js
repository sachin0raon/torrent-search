// Builds the absolute stream URL and per-player deep links.
//
// The stream URL is same-origin: the phone loaded this SPA from the public host
// (via nginx), so window.location.origin is exactly the origin the player must
// hit. The Go service only ever serves relative /stream/... paths.
//
// PLAYERS is the single place that knows each app's deep-link format. Adding a
// player is one entry. `build` receives (url, fileName): url is the absolute
// stream URL; fileName is the display file name (used by MX as the title).

const enc = encodeURIComponent;
const stripScheme = (url) => url.replace(/^https?:\/\//i, '');

export const PLAYERS = [
  // VLC: vlc:// prefix over the URL with the http(s) scheme removed.
  { id: 'vlc', label: 'VLC', build: (url) => `vlc://${stripScheme(url)}` },

  // MX Player (free): Android intent URI; the file name becomes the title. The
  // title value is URL-encoded so filenames with ';', '#' or spaces don't break
  // the intent URI (Android decodes the extra).
  {
    id: 'mx',
    label: 'MX Player',
    build: (url, fileName) =>
      `intent:${url}#Intent;package=com.mxtech.videoplayer.ad;S.title=${enc(fileName)};end`,
  },

  // nPlayer: nplayer-<url> over the full URL.
  { id: 'nplayer', label: 'nPlayer', build: (url) => `nplayer-${url}` },
];

// buildStreamUrl composes the absolute, same-origin URL for one file.
export function buildStreamUrl(sessionId, fileIndex, filename, origin = window.location.origin) {
  return `${origin}/stream/${sessionId}/${fileIndex}/${enc(filename)}`;
}

// playerLinks returns [{ id, label, href }] deep links for a stream URL.
export function playerLinks(streamUrl, fileName) {
  return PLAYERS.map((p) => ({ id: p.id, label: p.label, href: p.build(streamUrl, fileName) }));
}

// baseName returns the last path segment of a torrent file path.
export function baseName(path) {
  const parts = String(path).split('/');
  return parts[parts.length - 1] || path;
}
