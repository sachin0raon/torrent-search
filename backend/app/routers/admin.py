"""Admin user management: approval, status, password reset, and deletion."""
from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Request

from app.auth.db import connect, now_iso, revoke_sessions
from app.auth.deps import require_admin, require_csrf
from app.auth.schemas import PasswordReset
from app.auth.security import hash_password, validate_password

router = APIRouter(prefix="/api/admin", tags=["admin"])


def admin_guard(request: Request) -> dict:
    session = require_admin(request)
    require_csrf(request, session)
    return session


def _user(conn, user_id: int):
    row = conn.execute(
        "SELECT id, username, role, status, created_at, approved_at, password_changed_at"
        " FROM users WHERE id = ? AND role = 'user'",
        (user_id,),
    ).fetchone()
    if row is None:
        raise HTTPException(status_code=404, detail="User not found.")
    return row


def _payload(row) -> dict:
    return {
        "id": row["id"],
        "username": row["username"],
        "role": row["role"],
        "status": row["status"],
        "created_at": row["created_at"],
        "approved_at": row["approved_at"],
        "password_changed_at": row["password_changed_at"],
    }


@router.get("/users")
def list_users(session: dict = Depends(require_admin)):
    conn = connect()
    try:
        rows = conn.execute(
            "SELECT id, username, role, status, created_at, approved_at, password_changed_at"
            " FROM users WHERE role = 'user' ORDER BY created_at DESC, id DESC"
        ).fetchall()
    finally:
        conn.close()
    return {"users": [_payload(row) for row in rows]}


@router.post("/users/{user_id}/approve")
def approve(user_id: int, request: Request, session: dict = Depends(admin_guard)):
    conn = connect()
    try:
        _user(conn, user_id)
        cur = conn.execute(
            "UPDATE users SET status = 'active', approved_at = ?, approved_by = ?"
            " WHERE id = ? AND role = 'user' AND status = 'pending'",
            (now_iso(), session["user"]["id"], user_id),
        )
        if cur.rowcount == 0:
            raise HTTPException(status_code=409, detail="Only pending users can be approved.")
        conn.commit()
    finally:
        conn.close()
    return {"ok": True}


@router.post("/users/{user_id}/reject")
def reject(user_id: int, request: Request, session: dict = Depends(admin_guard)):
    conn = connect()
    try:
        _user(conn, user_id)
        cur = conn.execute(
            "UPDATE users SET status = 'rejected', approved_at = ?, approved_by = ?"
            " WHERE id = ? AND role = 'user' AND status = 'pending'",
            (now_iso(), session["user"]["id"], user_id),
        )
        if cur.rowcount == 0:
            raise HTTPException(status_code=409, detail="Only pending users can be rejected.")
        revoke_sessions(conn, user_id)
        conn.commit()
    finally:
        conn.close()
    return {"ok": True}


@router.post("/users/{user_id}/deactivate")
def deactivate(user_id: int, request: Request, session: dict = Depends(admin_guard)):
    conn = connect()
    try:
        _user(conn, user_id)
        cur = conn.execute(
            "UPDATE users SET status = 'disabled'"
            " WHERE id = ? AND role = 'user' AND status = 'active'",
            (user_id,),
        )
        if cur.rowcount == 0:
            raise HTTPException(status_code=409, detail="Only active users can be deactivated.")
        revoke_sessions(conn, user_id)
        conn.commit()
    finally:
        conn.close()
    return {"ok": True}


@router.post("/users/{user_id}/reactivate")
def reactivate(user_id: int, request: Request, session: dict = Depends(admin_guard)):
    conn = connect()
    try:
        _user(conn, user_id)
        cur = conn.execute(
            "UPDATE users SET status = 'active'"
            " WHERE id = ? AND role = 'user' AND status = 'disabled'",
            (user_id,),
        )
        if cur.rowcount == 0:
            raise HTTPException(status_code=409, detail="Only disabled users can be reactivated.")
        conn.commit()
    finally:
        conn.close()
    return {"ok": True}


@router.post("/users/{user_id}/password")
def reset_password(
    user_id: int,
    payload: PasswordReset,
    request: Request,
    session: dict = Depends(admin_guard),
):
    try:
        password = validate_password(payload.new_password)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    conn = connect()
    try:
        _user(conn, user_id)
        conn.execute(
            "UPDATE users SET password_hash = ?, password_changed_at = ? WHERE id = ?",
            (hash_password(password), now_iso(), user_id),
        )
        revoke_sessions(conn, user_id)
        conn.commit()
    finally:
        conn.close()
    return {"ok": True}


@router.delete("/users/{user_id}")
def delete_user(user_id: int, request: Request, session: dict = Depends(admin_guard)):
    conn = connect()
    try:
        _user(conn, user_id)
        conn.execute("DELETE FROM users WHERE id = ? AND role = 'user'", (user_id,))
        conn.commit()
    finally:
        conn.close()
    return {"ok": True}
