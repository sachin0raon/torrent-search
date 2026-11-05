"""SQLite storage for users, sessions, and auth metadata."""
from __future__ import annotations

import secrets
import sqlite3
from datetime import datetime, timedelta, timezone
from pathlib import Path

from app.auth.config import auth_db_path, get_session_ttl_seconds
from app.auth.security import hash_token


_SCHEMA = """
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'active', 'disabled', 'rejected')),
    created_at TEXT NOT NULL,
    approved_at TEXT,
    approved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    password_changed_at TEXT
);

CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
"""


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def connect() -> sqlite3.Connection:
    path = auth_db_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path, timeout=10)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys=ON")
    return conn


def init_db() -> None:
    conn = connect()
    try:
        conn.execute("PRAGMA journal_mode=WAL")
        conn.executescript(_SCHEMA)
        conn.execute(
            "INSERT OR IGNORE INTO auth_meta (key, value) VALUES ('schema_version', '1')"
        )
        conn.commit()
    finally:
        conn.close()


def create_session(conn: sqlite3.Connection, user_id: int) -> tuple[str, str, str]:
    """Insert a session and return (raw_token, csrf_token, expires_at)."""
    token = secrets.token_urlsafe(32)
    csrf = secrets.token_urlsafe(32)
    created = now_iso()
    expires = (
        datetime.now(timezone.utc) + timedelta(seconds=get_session_ttl_seconds())
    ).isoformat(timespec="seconds")
    conn.execute(
        "INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at)"
        " VALUES (?, ?, ?, ?, ?)",
        (hash_token(token), user_id, csrf, created, expires),
    )
    return token, csrf, expires


def revoke_sessions(conn: sqlite3.Connection, user_id: int) -> None:
    conn.execute("DELETE FROM sessions WHERE user_id = ?", (user_id,))


def revoke_session(conn: sqlite3.Connection, token_hash: str) -> None:
    conn.execute("DELETE FROM sessions WHERE token_hash = ?", (token_hash,))


def existing_username(conn: sqlite3.Connection, username: str) -> bool:
    row = conn.execute("SELECT 1 FROM users WHERE username = ?", (username,)).fetchone()
    return row is not None


def is_sqlite_path_writable() -> bool:
    path: Path = auth_db_path()
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        return True
    except OSError:
        return False
