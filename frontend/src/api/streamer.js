// Thin fetch wrapper for the Go streaming service (/stream-api/*), proxied by
// nginx to the localhost Go process. Errors carry the service's {error} message.

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

export const streamer = {
  // Add a magnet and wait for its metadata; returns
  // { sessionId, name, ready, files: [{ index, path, size, streamable }] }.
  createSession: (magnet, signal) =>
    request('/stream-api/sessions', { method: 'POST', body: { magnet }, signal }),

  // Poll an existing session (e.g. while metadata is still arriving).
  getSession: (id, signal) => request(`/stream-api/sessions/${id}`, { signal }),
};
