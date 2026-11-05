# Live auth + admin checks

Playwright suite for the authentication feature described in
[`docs/AUTH.md`](../docs/AUTH.md). It drives a real browser against a real
backend and asserts the full flow:

- anonymous search works, magnet copy stays public, Stream/Download prompt login
- signup creates a pending account; login shows the pending message
- admin approves, resets a password, deactivates/reactivates, and deletes users
- a normal user logs in, sees signed stream/download links, and changes their password
- a deactivated account is blocked, then reactivation restores access

These are **live end-to-end checks**, not unit tests. Search results, torrent
sources, the streamer, and the downloader are mocked at the network layer so no
trackers or torrents are touched. Every `/api/auth/*` and `/api/admin/*` call,
plus the nginx gating in front of them, is real.

## Prerequisites

- Docker (for the default disposable container)
- Node.js 18+ and npm
- Chromium for Playwright (`npm install` downloads it; otherwise run
  `npx playwright install chromium`)

## Quick start

```bash
cd testing
./run-auth-flow.sh
```

The runner:

1. builds `tsa-auth-test:latest` if it is missing (or with `BUILD=1`)
2. recreates the `tsa-auth-test` container with fresh `-data`/`-downloads` volumes
3. seeds an admin with a random password and prints it
4. waits for `/api/health`
5. installs npm dependencies on first run
6. runs the suite and leaves the container running for inspection

Screenshots land in `testing/screenshots/` (ignored by git).

To watch the run:

```bash
./run-auth-flow.sh --headed
```

## Running against an existing instance

```bash
PROVISION=0 ADMIN_PASS='your-admin-password' ./run-auth-flow.sh
# or point somewhere else
PROVISION=0 TARGET_URL=http://localhost:8000 ADMIN_PASS=... ./run-auth-flow.sh
```

The account must exist and be an active admin. The suite never creates or
modifies admin rows; it only manages normal users.

If you need to seed one manually (local non-Docker backend):

```bash
cd backend
python -m app.auth.cli hash-password --password 'your-admin-password'
# then insert with role='admin', status='active' — see docs/AUTH.md
```

## Configuration

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROVISION` | `1` | Recreate the Docker container before running |
| `BUILD` | `0` | Rebuild the image even if it exists |
| `IMAGE` | `tsa-auth-test:latest` | Docker image to build/run |
| `CONTAINER_NAME` | `tsa-auth-test` | Container name |
| `HOST_PORT` | `8090` | Host port mapped to the container's 8080 |
| `TARGET_URL` | `http://localhost:$HOST_PORT` | App origin |
| `ADMIN_USER` | `admin` | Admin username |
| `ADMIN_PASS` | generated when provisioning | Admin password; required with `PROVISION=0` |
| `KEEP_CONTAINER` | `1` | Set `0` to remove the container on exit |
| `RESET_VOLUMES` | `1` | Set `0` to keep the auth DB across provisioning |
| `DATA_VOLUME` | `${CONTAINER_NAME}-data` | Auth/config volume |
| `DOWNLOADS_VOLUME` | `${CONTAINER_NAME}-downloads` | Torrent download volume |
| `ALLOW_SHARED_VOLUMES` | `0` | Allow resetting volumes not named after the container |

Suite-level knobs (also usable when running `node test-auth-flow.cjs` directly):

| Variable | Default | Meaning |
|----------|---------|---------|
| `TEST_USER` | `run_<random>` | First account, approved during the run |
| `TEST_DELETE_USER` | `del_<random>` | Second account, deleted by the admin panel |
| `TEST_PASS` | `Testpass123!` | Registration password |
| `TEST_RESET_PASS` | `Resetpass789!` | Password set by the admin reset |
| `TEST_FINAL_PASS` | `Testpass456!` | Password set by the user |
| `SCREENSHOTS_DIR` | `./screenshots` next to the script | Screenshot output |
| `HEADED` | unset | `1` shows the browser (`--headed` works too) |
| `SLOWMO` | `40` (150 headed) | Milliseconds between Playwright actions |

## Repeat runs and rate limits

Usernames are randomized per run, and the suite deletes its own accounts at the
end (using the real admin API) even if a check fails. Repeated `PROVISION=1`
runs therefore start clean.

Registration is limited to **5 attempts per hour per IP** and login to 10 per
15 minutes per IP+username. The limiter is in-memory, so recreating the
container resets it. With `PROVISION=0`, about two runs fit in the registration
window before HTTP 429; restart the backend or wait an hour. Successful logins
reset the login counter.

## What is mocked vs real

| Real | Mocked |
|------|--------|
| `/api/auth/*` (register, login, me, logout, change-password) | `/api/search`, `/api/external-ids` |
| `/api/admin/*` (approve, reject, reset, deactivate, reactivate, delete) | `/api/torrentio`, `/api/comet`, `/api/meteor` |
| nginx cookie gating and `X-Client-Id` scoping | `/api/forum/search`, `/api/1337x/search` |
| Session cookie, CSRF token, password hashing | `/download-api/*`, `/stream-api/*` |
| Redirect/player link rendering | `/api/auth/stream-token` (returns `signed-test-token`) |

Because the token endpoint is mocked, the suite verifies that signed tokens are
requested and appended to links, not that nginx accepts a real signature. Token
minting/verification is covered by the backend tests.

## Troubleshooting

- **`no app reachable`** — with `PROVISION=0`, start the app first or use the
  default `PROVISION=1`.
- **`docker daemon is not reachable`** — start Docker; the runner needs it to
  create the disposable container.
- **Browser missing** — run `npm install` (downloads Chromium), or
  `npx playwright install chromium`.
- **429 during registration** — the per-IP registration cap was hit; provision a
  fresh container or wait for the window to clear.
- **A failing check leaves a container behind** — inspect with
  `docker logs tsa-auth-test`; remove with `docker rm -f tsa-auth-test`.
