"""Hash a password for the initial admin SQL bootstrap."""
from __future__ import annotations

import argparse
import getpass
import sys

from app.auth.security import hash_password, validate_password


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m app.auth.cli")
    sub = parser.add_subparsers(dest="command", required=True)
    hasher = sub.add_parser("hash-password", help="Print an Argon2id hash for a password")
    hasher.add_argument("--password", help="Password value (omit to prompt securely)")
    args = parser.parse_args(argv)

    if args.command == "hash-password":
        if args.password is not None:
            password = args.password
        else:
            password = getpass.getpass("Password: ")
            confirm = getpass.getpass("Confirm password: ")
            if password != confirm:
                print("Passwords do not match.", file=sys.stderr)
                return 1
        try:
            validate_password(password)
        except ValueError as exc:
            print(str(exc), file=sys.stderr)
            return 1
        print(hash_password(password))
        return 0
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
