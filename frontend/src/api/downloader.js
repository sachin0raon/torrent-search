// Thin fetch wrapper for the Go streamer's persistent download-manager
// feature (/download-api/*), proxied by nginx to the localhost Go process.
// Errors carry the service's {error} message. Absent (DOWNLOAD_ENGINE unset)
// deployments 404 on every route except /status, which always answers
// {enabled:false} — see docs/STREAMING.md §6.

async function request(path, { method = 'GET', body, signal } = {}) {
  const opts = { method, signal, headers: {} };
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  let data = null;
  try {
    data = await res.json();
  } catch {
    data = null;
  }
  if (!res.ok) {
    const detail = (data && data.error) || `Request failed (${res.status})`;
    const err = new Error(detail);
    err.status = res.status;
    throw err;
  }
  return data;
}

export const downloader = {
  // {enabled: bool} — always answerable, even when DOWNLOAD_ENGINE is unset.
  getStatus: (signal) => request('/download-api/status', { signal }),

  // Add a magnet and wait for its metadata; returns
  // { hash, name, files: [{ index, name, size }] }. Nothing downloads yet —
  // call selectFiles to actually start.
  createDownload: (magnet, signal) =>
    request('/download-api/torrents', { method: 'POST', body: { magnet }, signal }),

  // Promotes the given file indices to normal download priority. Additive:
  // does not affect files already selected.
  selectFiles: (hash, indices, signal) =>
    request(`/download-api/torrents/${hash}/select`, { method: 'POST', body: { indices }, signal }),

  // List every tracked download: [{ hash, name, state, progress, dlspeed, size }].
  listDownloads: (signal) => request('/download-api/torrents', { signal }),

  // One download's detail, including per-file progress/selection.
  getDownload: (hash, signal) => request(`/download-api/torrents/${hash}`, { signal }),

  // Removes the torrent and its downloaded data — always deletes files.
  deleteDownload: (hash, signal) =>
    request(`/download-api/torrents/${hash}`, { method: 'DELETE', signal }),
};
