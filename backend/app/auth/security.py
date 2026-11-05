"""Password hashing, validation, session tokens, and signed stream tokens."""
from __future__ import annotations

import base64
import hashlib
import hmac
import json
import re
import secrets
import time
from functools import lru_cache

from argon2 import PasswordHasher
from argon2.exceptions import InvalidHashError, VerificationError

from app.auth.config import get_signing_key_override

_USERNAME_RE = re.compile(r"^[A-Za-z0-9._-]{3,32}$")
_MAX_PASSWORD = 128

_hasher = PasswordHasher(time_cost=3, memory_cost=65536, parallelism=4)


def hash_password(password: str) -> str:
    return _hasher.hash(password)


def verify_password(password_hash: str, password: str) -> bool:
    try:
        return _hasher.verify(password_hash, password)
    except (VerificationError, InvalidHashError):
        return False


@lru_cache(maxsize=1)
def _dummy_hash() -> str:
    return _hasher.hash("not-a-real-password")


def dummy_verify(password: str) -> None:
    """Burn the same work as a real verification for unknown usernames."""
    verify_password(_dummy_hash(), password)


def validate_username(username: str) -> str:
    value = (username or "").strip()
    if not _USERNAME_RE.fullmatch(value):
        raise ValueError(
            "Username must be 3-32 characters using letters, digits, dot, underscore, or hyphen."
        )
    return value


def validate_password(password: str) -> str:
    if not isinstance(password, str) or not 8 <= len(password) <= _MAX_PASSWORD:
        raise ValueError("Password must be 8-128 characters.")
    if not password.strip():
        raise ValueError("Password must not be blank.")
    return password


def new_token() -> str:
    return secrets.token_urlsafe(32)


def hash_token(token: str) -> str:
    return hashlib.sha256(token.encode("utf-8")).hexdigest()


def _b64e(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


def _b64d(value: str) -> bytes:
    padding = "=" * (-len(value) % 4)
    return base64.urlsafe_b64decode(value + padding)


@lru_cache(maxsize=1)
def signing_key() -> str:
    override = get_signing_key_override()
    if override:
        return override

    from app.auth.db import connect

    conn = connect()
    try:
        conn.execute(
            "INSERT OR IGNORE INTO auth_meta (key, value) VALUES ('stream_signing_key', ?)",
            (secrets.token_urlsafe(48),),
        )
        row = conn.execute(
            "SELECT value FROM auth_meta WHERE key = 'stream_signing_key'"
        ).fetchone()
        conn.commit()
        if row is None:
            raise RuntimeError("Could not initialize stream signing key")
        return row["value"]
    finally:
        conn.close()


def mint_stream_token(user_id: int, kind: str, scope: str, ttl_seconds: int) -> tuple[str, int]:
    expires = int(time.time()) + ttl_seconds
    payload = {
        "v": 1,
        "uid": user_id,
        "kind": kind,
        "scope": scope,
        "exp": expires,
    }
    body = _b64e(json.dumps(payload, separators=(",", ":")).encode("utf-8"))
    sig = _b64e(
        hmac.new(signing_key().encode("utf-8"), body.encode("ascii"), hashlib.sha256).digest()
    )
    return f"{body}.{sig}", expires


def verify_stream_token(token: str) -> dict | None:
    if not isinstance(token, str) or token.count(".") != 1:
        return None
    body, sig = token.split(".", 1)
    expected = _b64e(
        hmac.new(signing_key().encode("utf-8"), body.encode("ascii"), hashlib.sha256).digest()
    )
    if not hmac.compare_digest(sig, expected):
        return None
    try:
        payload = json.loads(_b64d(body).decode("utf-8"))
    except (ValueError, UnicodeDecodeError, json.JSONDecodeError):
        return None
    if not isinstance(payload, dict) or payload.get("v") != 1:
        return None
    if not isinstance(payload.get("exp"), int) or payload["exp"] <= int(time.time()):
        return None
    return payload
