// CSRF token for the current server-side session. Set from /api/auth/me or a
// successful login; sent as X-CSRF-Token on state-changing requests.
let csrfToken = '';

export function getCsrfToken() {
  return csrfToken;
}

export function setCsrfToken(value) {
  csrfToken = value || '';
}
