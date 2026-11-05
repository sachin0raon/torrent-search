"""Authentication configuration helpers."""
from __future__ import annotations

import os
from pathlib import Path

from fastapi import Request


def _env_flag(name: str, default: bool) -> bool:
    raw = os.environ.get(name)
    if raw is None or not raw.strip():
        return default
    return raw.strip().lower() in ("1", "true", "yes", "on")


def auth_db_path() -> Path:
    """SQLite file path. /data exists in the Docker image; local runs use backend/auth.db."""
    override = os.environ.get("AUTH_DB_PATH", "").strip()
    if override:
        return Path(override).expanduser()
    if Path("/data").is_dir():
        return Path("/data/auth.db")
    return Path(__file__).resolve().parents[2] / "auth.db"


def get_session_ttl_seconds() -> int:
    return max(60, int(os.environ.get("AUTH_SESSION_TTL_SECONDS", "604800")))


def get_stream_token_ttl_seconds() -> int:
    return max(60, int(os.environ.get("AUTH_STREAM_TOKEN_TTL_SECONDS", "43200")))


def get_signing_key_override() -> str | None:
    return os.environ.get("AUTH_SIGNING_KEY", "").strip() or None


def get_streamer_base_url() -> str:
    return os.environ.get("STREAMER_BASE_URL", "http://127.0.0.1:8001").rstrip("/")


# The Go streamer scopes data by the X-Client-Id tag. nginx builds this from
# the auth subrequest's X-User-Id ("user:<id>") by prepending "acct-", so keep
# this helper in sync with deploy/nginx.conf.
def client_scope(user_id: int) -> str:
    return f"acct-user:{user_id}"


def cookie_secure(request: Request) -> bool:
    """Secure cookies automatically behind HTTPS, overridable for local runs."""
    override = os.environ.get("AUTH_COOKIE_SECURE")
    if override is not None and override.strip():
        return _env_flag("AUTH_COOKIE_SECURE", False)
    forwarded = request.headers.get("x-forwarded-proto", "").split(",")[0].strip().lower()
    return forwarded == "https" or request.url.scheme == "https"
