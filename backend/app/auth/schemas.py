"""Pydantic request schemas for auth and admin routes."""
from __future__ import annotations

from typing import Literal

from pydantic import BaseModel


class Credentials(BaseModel):
    username: str
    password: str


class ChangePassword(BaseModel):
    current_password: str
    new_password: str


class StreamTokenRequest(BaseModel):
    kind: Literal["stream", "download"]
    id: str


class PasswordReset(BaseModel):
    new_password: str
