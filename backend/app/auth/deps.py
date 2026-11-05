"""Request dependencies: current session, role checks, CSRF, and cookies."""
from __future__ import annotations

import hmac
from datetime import datetime, timezone

from fastapi import HTTPException, Request, Response

from app.auth.config import cookie_secure, get_session_ttl_seconds
from app.auth.db import connect
from app.auth.security import hash_token

SESSION_COOKIE = "tsa_session"


def current_session(request: Request) -> dict | None:
    token = request.cookies.get(SESSION_COOKIE)
    if not token:
        return None
    conn = connect()
    try:
        row = conn.execute(
            """
            SELECT s.token_hash, s.csrf_token, s.expires_at,
                   u.id AS user_id, u.username, u.role, u.status
            FROM sessions s
            JOIN users u ON u.id = s.user_id
            WHERE s.token_hash = ?
            """,
            (hash_token(token),),
        ).fetchone()
    finally:
        conn.close()
    if row is None:
        return None
    expires = row["expires_at"]
    if expires <= datetime.now(timezone.utc).isoformat(timespec="seconds"):
        return None
    if row["status"] != "active":
        return None
    return {
        "token_hash": row["token_hash"],
        "csrf_token": row["csrf_token"],
        "expires_at": expires,
        "user": {
            "id": row["user_id"],
            "username": row["username"],
            "role": row["role"],
        },
    }


def require_user(request: Request) -> dict:
    session = current_session(request)
    if session is None:
        raise HTTPException(status_code=401, detail="Authentication required.")
    return session


def require_admin(request: Request) -> dict:
    session = require_user(request)
    if session["user"]["role"] != "admin":
        raise HTTPException(status_code=403, detail="Admin access required.")
    return session


def require_csrf(request: Request, session: dict) -> None:
    provided = request.headers.get("x-csrf-token", "")
    if not provided or not hmac.compare_digest(provided, session["csrf_token"]):
        raise HTTPException(status_code=403, detail="Invalid CSRF token.")


def set_session_cookie(response: Response, request: Request, token: str) -> None:
    response.set_cookie(
        key=SESSION_COOKIE,
        value=token,
        max_age=get_session_ttl_seconds(),
        httponly=True,
        samesite="lax",
        secure=cookie_secure(request),
        path="/",
    )


def clear_session_cookie(response: Response, request: Request) -> None:
    response.delete_cookie(
        key=SESSION_COOKIE,
        httponly=True,
        samesite="lax",
        secure=cookie_secure(request),
        path="/",
    )


def client_ip(request: Request) -> str:
    forwarded = request.headers.get("x-forwarded-for", "").split(",")[0].strip()
    if forwarded:
        return forwarded
    return request.client.host if request.client else "unknown"
