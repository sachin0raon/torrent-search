# Authentication, Signup Approval, and Admin Panel

Status: implemented design summary. Search stays public; only Stream and Download require login.

## Decision log

| # | Decision | Why |
|---|----------|-----|
| 1 | Gate Stream + Download only; magnet copy stays public | Keeps search and sharing behavior unchanged |
| 2 | SQLite at `/data/auth.db` | Local single instance, durable, raw SQL works |
| 3 | HttpOnly cookie + server-side sessions | Immediate revocation on reset/deactivate/delete |
| 4 | HMAC-signed URL token for players | VLC/MX cannot send cookies; real session token never enters URLs |
| 5 | Torrents scoped to account, not browser | Cross-device ownership; matches login model |
| 6 | Username + password only, no email | No email delivery needed |
| 7 | Admin sets a new password (never displayed again) | Simple reset flow; all sessions revoked |
| 8 | Users can change their own password | Current password required; other sessions revoked |
| 9 | Dedicated `/admin` page, normal users only | Admins are created by SQL, not by the panel |
| 10 | Approve or reject pending registrations | Clear login messages for both outcomes |
| 11 | Deactivation/deletion leaves torrents running | No surprise file deletion; idle/retention rules apply |
| 12 | nginx `auth_request` + FastAPI verification | One enforcement point; Go stays loopback-only |
| 13 | Argon2id password hashes | Strong, current default |
| 14 | 7-day sessions; 12-hour stream tokens | Usable playback without frequent re-login |
| 15 | Go streamer unchanged | nginx injects account-scoped `X-Client-Id` |
| 16 | Two recovery paths: pending and rejected | Registration flow stays simple and explicit |

## Schema

```sql
users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user'
    CHECK (role IN ('user', 'admin')),
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'active', 'disabled', 'rejected')),
  created_at TEXT NOT NULL,
  approved_at TEXT,
  approved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  password_changed_at TEXT
);

sessions (
  id INTEGER PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

auth_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

`auth_meta` stores the schema version and the generated signing key when
`AUTH_SIGNING_KEY` is unset. Session tokens are stored hashed (SHA-256).

## Login status messages

| Status | Message |
|--------|---------|
| `pending` | Registration is awaiting admin approval. |
| `rejected` | Registration was rejected by an admin. |
| `disabled` | This account has been deactivated. |
| bad credentials | Invalid username or password. |

## API routes

- `POST /api/auth/register`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/me`
- `POST /api/auth/change-password`
- `POST /api/auth/stream-token`
- `GET /api/auth/verify` (internal nginx auth subrequest)
- `GET /api/auth/verify-stream` (internal nginx auth subrequest)
- `GET /api/admin/users`
- `POST /api/admin/users/{id}/approve`
- `POST /api/admin/users/{id}/reject`
- `POST /api/admin/users/{id}/reactivate`
- `POST /api/admin/users/{id}/deactivate`
- `POST /api/admin/users/{id}/password`
- `DELETE /api/admin/users/{id}`

All `/api/admin/*` routes require role `admin`; admin rows are excluded from
management.

## Rate limiting

- Login: 10 attempts per 15 minutes per IP + username.
- Registration: 5 attempts per hour per IP.
- Exceeding either limit returns HTTP 429.

## nginx gate

- `/stream-api/` and `/download-api/` JSON routes verify the session cookie,
  except `GET /download-api/status`, which stays public so logged-out users
  still see the Download action and get the login prompt.
- `/stream/` and `/download-api/stream/` verify the signed `?token=`.
- Signed token is `base64url(JSON {v,uid,kind,scope,exp})` + `.` +
  `base64url(HMAC-SHA256)`, keyed by the signing key; `v` is `1`. Scope is
  `stream:<sessionId>` or `download:<hash>`.
- Verification returns `X-User-Id: user:<id>`; nginx sets
  `X-Client-Id: acct-user:<id>` before proxying to the Go streamer. The
  `acct-` prefix keeps the value non-empty so a missing auth header cannot be
  interpreted as unscoped access. The backend's own ownership probe uses the
  same `acct-user:<id>` value (see `client_scope` in `app/auth/config.py`).

## CORS

Credentialed requests are allowed from the explicit `CORS_ORIGINS` allowlist
with GET/POST/PUT/PATCH/DELETE. Same-origin nginx serving does not need CORS;
this only covers development or other cross-origin clients.

## Environment variables

| Variable | Default | Notes |
|----------|---------|-------|
| `AUTH_DB_PATH` | `/data/auth.db` in Docker; `backend/auth.db` locally | SQLite file |
| `AUTH_SIGNING_KEY` | generated and stored in DB | Override to share a key across instances |
| `AUTH_SESSION_TTL_SECONDS` | `604800` (7 days) | Absolute session lifetime |
| `AUTH_STREAM_TOKEN_TTL_SECONDS` | `43200` (12 hours) | Player URL token lifetime |
| `AUTH_COOKIE_SECURE` | auto from `X-Forwarded-Proto` | Set `true`/`false` to override |
| `STREAMER_BASE_URL` | `http://127.0.0.1:8001` | Used to verify download ownership before minting tokens |

## Create the first admin

Generate an Argon2id hash:

```bash
cd backend
.venv/bin/python -m app.auth.cli hash-password
```

Insert the admin, replacing `<hash>` with the printed value:

```sql
INSERT INTO users (username, password_hash, role, status, created_at)
VALUES ('admin', '<hash>', 'admin', 'active', datetime('now'));
```

Run this against `AUTH_DB_PATH` (inside Docker: `/data/auth.db`).

## Accepted risks

- Production enforcement depends on nginx. The Go streamer must remain bound to
  `127.0.0.1`.
- Stream tokens prove a logged-in account and bind to an unguessable session ID;
  they do not add a second per-session ownership check.
- Signed player URLs are stateless and stay valid until their 12-hour expiry,
  even after logout, deactivation, or deletion. Lower
  `AUTH_STREAM_TOKEN_TTL_SECONDS` to tighten that window.
- Account scoping hides pre-existing anonymous browser-scoped torrents; they
  follow the existing idle/retention cleanup.
