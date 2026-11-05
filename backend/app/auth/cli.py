"""Admin bootstrap commands: hash a password or insert/update an admin user."""
from __future__ import annotations

import argparse
import getpass
import sys

from app.auth.config import auth_db_path
from app.auth.db import connect, init_db, now_iso, revoke_sessions
from app.auth.security import hash_password, validate_password, validate_username


def _read_password(args: argparse.Namespace) -> str | None:
    if args.password_stdin:
        password = sys.stdin.readline().rstrip("\r\n")
        if not password:
            print("No password received on stdin.", file=sys.stderr)
            return None
        return password
    if args.password is not None:
        return args.password
    password = getpass.getpass("Password: ")
    confirm = getpass.getpass("Confirm password: ")
    if password != confirm:
        print("Passwords do not match.", file=sys.stderr)
        return None
    return password


def _insert_admin(args: argparse.Namespace) -> int:
    try:
        username = validate_username(args.username)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    password = _read_password(args)
    if password is None:
        return 1
    try:
        validate_password(password)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    init_db()
    conn = connect()
    try:
        row = conn.execute(
            "SELECT id, role FROM users WHERE username = ?", (username,)
        ).fetchone()
        stamp = now_iso()
        if row is None:
            conn.execute(
                "INSERT INTO users (username, password_hash, role, status, created_at, approved_at)"
                " VALUES (?, ?, 'admin', 'active', ?, ?)",
                (username, hash_password(password), stamp, stamp),
            )
            action = "Created"
        else:
            if row["role"] != "admin":
                print(
                    f"User '{username}' already exists and is not an admin;"
                    " choose another username.",
                    file=sys.stderr,
                )
                return 1
            if not args.update:
                print(
                    f"Admin '{username}' already exists; pass --update to set a new password.",
                    file=sys.stderr,
                )
                return 1
            conn.execute(
                "UPDATE users SET password_hash = ?, status = 'active',"
                " password_changed_at = ? WHERE id = ?",
                (hash_password(password), stamp, row["id"]),
            )
            revoke_sessions(conn, row["id"])
            action = "Updated"
        conn.commit()
    finally:
        conn.close()

    print(f"{action} admin '{username}' in {auth_db_path()}.")
    if action == "Updated":
        print("All sessions for this admin were revoked.")
    return 0


def _add_password_args(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--password", help="Password value (omit to prompt securely)")
    parser.add_argument(
        "--password-stdin",
        action="store_true",
        help="Read the password from stdin (for scripts)",
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m app.auth.cli")
    sub = parser.add_subparsers(dest="command", required=True)

    hasher = sub.add_parser("hash-password", help="Print an Argon2id hash for a password")
    _add_password_args(hasher)

    admin = sub.add_parser(
        "insert-admin",
        help="Create an admin user, or update one with --update",
    )
    admin.add_argument("--username", default="admin", help="Admin username (default: admin)")
    _add_password_args(admin)
    admin.add_argument(
        "--update",
        action="store_true",
        help="Set a new password for an existing admin and revoke its sessions",
    )

    args = parser.parse_args(argv)
    if args.password is not None and args.password_stdin:
        parser.error("--password and --password-stdin cannot be used together")

    if args.command == "hash-password":
        password = _read_password(args)
        if password is None:
            return 1
        try:
            validate_password(password)
        except ValueError as exc:
            print(str(exc), file=sys.stderr)
            return 1
        print(hash_password(password))
        return 0

    if args.command == "insert-admin":
        return _insert_admin(args)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
