"""Login, registration, sessions, and signed stream-token issuance."""
from __future__ import annotations

import sqlite3
import threading
import time
from datetime import datetime, timezone
from urllib.parse import parse_qs, quote, unquote, urlsplit

from fastapi import APIRouter, Depends, HTTPException, Request, Response

from app.auth.config import (
    client_scope,
    get_streamer_base_url,
    get_stream_token_ttl_seconds,
)
from app.auth.db import (
    connect,
    create_session,
    existing_username,
    now_iso,
    revoke_session,
    revoke_sessions,
)
from app.auth.deps import (
    client_ip,
    clear_session_cookie,
    current_session,
    require_csrf,
    require_user,
    set_session_cookie,
)
from app.auth.schemas import ChangePassword, Credentials, StreamTokenRequest
from app.auth.security import (
    dummy_verify,
    hash_password,
    mint_stream_token,
    validate_password,
    validate_username,
    verify_password,
    verify_stream_token,
)

router = APIRouter(prefix="/api/auth", tags=["auth"])

_STATUS_MESSAGES = {
    "pending": "Registration is awaiting admin approval.",
    "rejected": "Registration was rejected by an admin.",
    "disabled": "This account has been deactivated.",
}


class WindowLimiter:
    def __init__(self, max_events: int, window_seconds: int):
        self.max_events = max_events
        self.window_seconds = window_seconds
        self._events: dict[str, list[float]] = {}
        self._lock = threading.Lock()

    def _prune(self, key: str, now: float) -> list[float]:
        cutoff = now - self.window_seconds
        events = [t for t in self._events.get(key, []) if t > cutoff]
        if events:
            self._events[key] = events
        else:
            self._events.pop(key, None)
        return events

    def allow(self, key: str) -> bool:
        now = time.monotonic()
        with self._lock:
            return len(self._prune(key, now)) < self.max_events

    def record(self, key: str) -> None:
        now = time.monotonic()
        with self._lock:
            self._events.setdefault(key, []).append(now)

    def reset(self, key: str) -> None:
        with self._lock:
            self._events.pop(key, None)


login_limiter = WindowLimiter(10, 15 * 60)
register_limiter = WindowLimiter(5, 60 * 60)


def _user_payload(row) -> dict:
    return {"id": row["id"], "username": row["username"], "role": row["role"]}


@router.post("/register", status_code=201)
def register(payload: Credentials, request: Request):
    ip = client_ip(request)
    key = f"register:{ip}"
    if not register_limiter.allow(key):
        raise HTTPException(status_code=429, detail="Too many registration attempts. Try again later.")
    register_limiter.record(key)

    try:
        username = validate_username(payload.username)
        password = validate_password(payload.password)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    conn = connect()
    try:
        if existing_username(conn, username):
            raise HTTPException(status_code=409, detail="Username is already taken.")
        try:
            conn.execute(
                "INSERT INTO users (username, password_hash, role, status, created_at)"
                " VALUES (?, ?, 'user', 'pending', ?)",
                (username, hash_password(password), now_iso()),
            )
            conn.commit()
        except sqlite3.IntegrityError as exc:
            raise HTTPException(status_code=409, detail="Username is already taken.") from exc
    finally:
        conn.close()

    return {
        "status": "pending",
        "message": "Registration submitted. An admin must approve it before you can log in.",
    }


@router.post("/login")
def login(payload: Credentials, request: Request, response: Response):
    username = (payload.username or "").strip()
    key = f"login:{client_ip(request)}:{username.lower()}"
    if not login_limiter.allow(key):
        raise HTTPException(status_code=429, detail="Too many login attempts. Try again later.")

    conn = connect()
    try:
        row = conn.execute(
            "SELECT id, username, password_hash, role, status FROM users WHERE username = ?",
            (username,),
        ).fetchone()
        if row is None:
            dummy_verify(payload.password)
            login_limiter.record(key)
            raise HTTPException(status_code=401, detail="Invalid username or password.")
        if not verify_password(row["password_hash"], payload.password):
            login_limiter.record(key)
            raise HTTPException(status_code=401, detail="Invalid username or password.")
        if row["status"] in _STATUS_MESSAGES:
            raise HTTPException(status_code=403, detail=_STATUS_MESSAGES[row["status"]])

        token, csrf, _expires = create_session(conn, row["id"])
        conn.commit()
        login_limiter.reset(key)
        user_payload = {"id": row["id"], "username": row["username"], "role": row["role"]}
    finally:
        conn.close()

    set_session_cookie(response, request, token)
    return {"user": user_payload, "csrf_token": csrf}


@router.post("/logout", status_code=204)
def logout(request: Request):
    session = current_session(request)
    response = Response(status_code=204)
    if session is not None:
        require_csrf(request, session)
        conn = connect()
        try:
            revoke_session(conn, session["token_hash"])
            conn.commit()
        finally:
            conn.close()
    clear_session_cookie(response, request)
    return response


@router.get("/me")
def me(session: dict = Depends(require_user)):
    return {"user": session["user"], "csrf_token": session["csrf_token"]}


@router.post("/change-password")
def change_password(
    payload: ChangePassword,
    request: Request,
    response: Response,
    session: dict = Depends(require_user),
):
    require_csrf(request, session)
    try:
        new_password = validate_password(payload.new_password)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    conn = connect()
    try:
        row = conn.execute(
            "SELECT password_hash FROM users WHERE id = ?", (session["user"]["id"],)
        ).fetchone()
        if row is None or not verify_password(row["password_hash"], payload.current_password):
            raise HTTPException(status_code=400, detail="Current password is incorrect.")
        conn.execute(
            "UPDATE users SET password_hash = ?, password_changed_at = ? WHERE id = ?",
            (hash_password(new_password), now_iso(), session["user"]["id"]),
        )
        revoke_sessions(conn, session["user"]["id"])
        token, csrf, _expires = create_session(conn, session["user"]["id"])
        conn.commit()
    finally:
        conn.close()

    set_session_cookie(response, request, token)
    return {"user": session["user"], "csrf_token": csrf}


@router.post("/stream-token")
async def stream_token(
    payload: StreamTokenRequest,
    request: Request,
    session: dict = Depends(require_user),
):
    require_csrf(request, session)
    scope_id = (payload.id or "").strip()
    if not scope_id or len(scope_id) > 160 or any(
        c not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._~-"
        for c in scope_id
    ):
        raise HTTPException(status_code=400, detail="Invalid token scope.")

    user_id = session["user"]["id"]
    client = request.app.state.http
    base = get_streamer_base_url()
    path = (
        f"/stream-api/sessions/{quote(scope_id, safe='')}"
        if payload.kind == "stream"
        else f"/download-api/torrents/{quote(scope_id, safe='')}"
    )
    try:
        upstream = await client.get(base + path, headers={"X-Client-Id": client_scope(user_id)})
    except Exception as exc:  # transport failure
        raise HTTPException(status_code=503, detail="Stream service unavailable.") from exc
    if upstream.status_code in (404, 410):
        raise HTTPException(status_code=404, detail="Torrent is no longer available.")
    if upstream.status_code != 200:
        raise HTTPException(status_code=503, detail="Could not verify torrent ownership.")

    scope = f"{payload.kind}:{scope_id}"
    ttl = get_stream_token_ttl_seconds()
    token, expires = mint_stream_token(user_id, payload.kind, scope, ttl)
    return {
        "token": token,
        "expires_at": datetime.fromtimestamp(expires, timezone.utc).isoformat(timespec="seconds"),
    }


@router.get("/verify")
def verify(request: Request):
    session = current_session(request)
    if session is None:
        raise HTTPException(status_code=401, detail="Authentication required.")
    return Response(
        status_code=204,
        headers={"X-User-Id": f"user:{session['user']['id']}"},
    )


@router.get("/verify-stream")
def verify_stream(request: Request):
    uri = request.headers.get("x-original-uri") or request.url.path
    parsed = urlsplit(uri)
    token = parse_qs(parsed.query).get("token", [None])[0]
    if not token:
        raise HTTPException(status_code=401, detail="Missing stream token.")

    parts = unquote(parsed.path).strip("/").split("/")
    if len(parts) >= 2 and parts[0] == "stream":
        kind, scope_id = "stream", parts[1]
    elif len(parts) >= 3 and parts[0] == "download-api" and parts[1] == "stream":
        kind, scope_id = "download", parts[2]
    else:
        raise HTTPException(status_code=401, detail="Invalid stream path.")

    payload = verify_stream_token(token)
    if (
        payload is None
        or payload.get("kind") != kind
        or payload.get("scope") != f"{kind}:{scope_id}"
    ):
        raise HTTPException(status_code=401, detail="Invalid or expired stream token.")

    return Response(
        status_code=204,
        headers={"X-User-Id": f"user:{payload['uid']}"},
    )
