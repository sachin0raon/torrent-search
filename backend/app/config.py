"""Configuration: environment defaults + runtime-overridable forum base URL.

Precedence for the forum base URL:
    config.json value (if present & non-empty)  ->  FORUM_BASE_URL from .env

The config.json file is written when the user overrides the base URL from the UI,
so the override survives restarts. Values are re-read per request (cheap, always fresh).
"""
from __future__ import annotations

import json
import os
from pathlib import Path
from typing import NamedTuple
from urllib.parse import urlparse

from dotenv import load_dotenv

load_dotenv()

# config.json lives at the backend root (next to pyproject.toml), regardless of CWD.
_DEFAULT_CONFIG_PATH = Path(__file__).resolve().parent.parent / "config.json"


def config_path() -> Path:
    """Resolve the config.json path, honoring CONFIG_JSON_PATH (used by tests).

    Read dynamically per call so tests can point it at a temp file without
    reloading this module.
    """
    override = os.environ.get("CONFIG_JSON_PATH")
    return Path(override) if override else _DEFAULT_CONFIG_PATH


CORS_ORIGINS = ["http://localhost:5173", "http://127.0.0.1:5173"]
OUTBOUND_TIMEOUT_SECONDS = 10.0

# Some providers (e.g. torrentio behind Cloudflare) reject the default
# python-httpx User-Agent with a 403. Present a browser-like UA on outbound calls.
REQUEST_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
    ),
    "Accept": "*/*",
    "Accept-Language": "en-US,en;q=0.9",
}

TORRENTIO_BASE = "https://torrentio.strem.fun/sort=seeders|qualityfilter=480p/stream"

# Comet/Meteor are Stremio-addon-style scrapers configured via a base64-encoded
# JSON blob baked into the URL path (resolution filters, result format, etc.),
# rather than Torrentio's pipe-separated query segment.
COMET_BASE = (
    "https://comet.feels.legal/"
    "eyJtYXhSZXN1bHRzUGVyUmVzb2x1dGlvbiI6MCwibWF4U2l6ZSI6MCwiY2FjaGVkT25seSI6ZmFsc2UsInNvcnRDYWNoZWRVbmNhY2hlZFRvZ2V0aGVyIjp0cnVlLCJyZW1vdmVUcmFzaCI6ZmFsc2UsInJlc3VsdEZvcm1hdCI6WyJ0aXRsZSIsInNlZWRlcnMiLCJzaXplIiwidHJhY2tlciJdLCJkZWJyaWRTZXJ2aWNlcyI6W10sImVuYWJsZVRvcnJlbnQiOnRydWUsImRlZHVwbGljYXRlU3RyZWFtcyI6ZmFsc2UsInNjcmFwZURlYnJpZEFjY291bnRUb3JyZW50cyI6ZmFsc2UsImRlYnJpZFN0cmVhbVByb3h5UGFzc3dvcmQiOiIiLCJsYW5ndWFnZXMiOnsicmVxdWlyZWQiOltdLCJhbGxvd2VkIjpbXSwiZXhjbHVkZSI6W10sInByZWZlcnJlZCI6W119LCJyZXNvbHV0aW9ucyI6eyJyNTc2cCI6ZmFsc2UsInI0ODBwIjpmYWxzZSwicjM2MHAiOmZhbHNlLCJyMjQwcCI6ZmFsc2V9LCJvcHRpb25zIjp7InJlbW92ZV9yYW5rc191bmRlciI6LTEwMDAwMDAwMDAwLCJhbGxvd19lbmdsaXNoX2luX2xhbmd1YWdlcyI6ZmFsc2UsInJlbW92ZV91bmtub3duX2xhbmd1YWdlcyI6ZmFsc2V9fQ=="
    "/stream"
)
METEOR_BASE = (
    "https://meteorfortheweebs.midnightignite.me/"
    "eyJjYWNoZWRPbmx5IjpmYWxzZSwic2tpcFJlbGVhc2VGaWx0ZXIiOnRydWUsInJlbW92ZVRyYXNoIjpmYWxzZSwicmVtb3ZlU2FtcGxlcyI6ZmFsc2UsImFsbG93QWR1bHQiOnRydWUsImV4Y2x1ZGUzRCI6ZmFsc2UsImVuYWJsZVNlYURleCI6ZmFsc2UsInNob3dZb3VyTWVkaWEiOmZhbHNlLCJtaW5TZWVkZXJzIjowLCJtYXhSZXN1bHRzIjowLCJtYXhQZXJSZXNvbHV0aW9uIjowLCJyZXNvbHV0aW9ucyI6WyI0ayIsIjEwODBwIiwiNzIwcCJdLCJwcmVmZXJyZWRMYW5ncyI6WyJlbiIsIm11bHRpIl0sImxhbmd1YWdlcyI6W10sImV4Y2x1ZGVkTGFuZ3MiOltdLCJyZXN1bHRGb3JtYXQiOlsidGl0bGUiLCJxdWFsaXR5Iiwic2l6ZSIsImF1ZGlvIl0sImxhbmd1YWdlRm9ybWF0IjoiY29kZXMiLCJzb3J0T3JkZXIiOlsic2VlZGVycyIsImNhY2hlZCIsInJlc29sdXRpb24iLCJxdWFsaXR5IiwibGFuZ3VhZ2UiLCJzaXplIiwic2VhZGV4IiwicGFjayJdLCJhbGxvd1AyUCI6ZmFsc2UsImV4Y2x1ZGVkU291cmNlcyI6W119"
    "/stream"
)


class RetrySettings(NamedTuple):
    max_attempts: int
    backoff_base: float  # initial backoff seconds
    backoff_cap: float   # max backoff seconds per wait
    jitter: float        # random jitter added to each wait (seconds)
    deadline: float      # overall time budget across all attempts (seconds)


class ProbeSettings(NamedTuple):
    enabled: bool
    interval_minutes: int
    query: str


def _env_flag(name: str, default: bool) -> bool:
    raw = os.environ.get(name)
    if raw is None:
        return default
    return raw.strip().lower() in ("1", "true", "yes", "on")


def get_forum_probe_settings() -> ProbeSettings:
    """Background forum-URL probe policy; read from env so it's tunable per deploy.

    The probe periodically searches the configured base URL and persists any
    redirected origin, so a moved forum domain is picked up even while idle
    (not only on the next user search).
    """
    return ProbeSettings(
        enabled=_env_flag("FORUM_PROBE_ENABLED", True),
        interval_minutes=max(1, int(os.environ.get("FORUM_PROBE_INTERVAL_MINUTES", "30"))),
        query=(os.environ.get("FORUM_PROBE_QUERY", "a").strip() or "a"),
    )


def get_discover_cache_ttl_seconds() -> float:
    """TTL for the in-process Discover (trending/popular/top-rated) cache."""
    return float(os.environ.get("DISCOVER_CACHE_TTL_SECONDS", "3600"))


def get_retry_settings() -> RetrySettings:
    """Retry policy for outbound GET calls; read from env so it's tunable per deploy."""
    return RetrySettings(
        max_attempts=int(os.environ.get("EXTERNAL_MAX_ATTEMPTS", "3")),
        backoff_base=float(os.environ.get("EXTERNAL_BACKOFF_BASE", "0.3")),
        backoff_cap=float(os.environ.get("EXTERNAL_BACKOFF_CAP", "3.0")),
        jitter=float(os.environ.get("EXTERNAL_BACKOFF_JITTER", "0.5")),
        deadline=float(os.environ.get("EXTERNAL_RETRY_DEADLINE", "15.0")),
    )


class ConfigError(ValueError):
    """Raised when a configuration value is invalid."""


def get_tmdb_api_key() -> str:
    key = os.environ.get("TMDB_API_KEY", "").strip()
    if not key:
        raise ConfigError("TMDB_API_KEY is not set in the environment (.env)")
    return key


def normalize_base_url(url: str) -> str:
    """Validate an http(s) URL and strip any trailing slash.

    Raises ConfigError if the URL is malformed.
    """
    url = (url or "").strip()
    if not url:
        raise ConfigError("Base URL must not be empty")
    parsed = urlparse(url)
    if parsed.scheme not in ("http", "https") or not parsed.netloc:
        raise ConfigError("Base URL must be a valid http(s) URL")
    return url.rstrip("/")


def _read_config_json() -> dict:
    path = config_path()
    if not path.exists():
        return {}
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return {}


def get_forum_base_url() -> tuple[str | None, str]:
    """Return (base_url_or_None, source) where source is 'config' or 'env'.

    base_url is None only if neither config.json nor .env provide a usable value.
    """
    override = (_read_config_json().get("forum_base_url") or "").strip()
    if override:
        return override.rstrip("/"), "config"

    env_default = os.environ.get("FORUM_BASE_URL", "").strip()
    if env_default:
        return env_default.rstrip("/"), "env"

    return None, "env"


def get_file_browser_url() -> str | None:
    """Return the FILE_BROWSER_URL env var, or None if unset/empty."""
    return os.environ.get("FILE_BROWSER_URL", "").strip() or None


def set_forum_base_url(url: str) -> str:
    """Validate, normalize, and persist the forum base URL override to config.json.

    Returns the normalized URL that was saved.
    """
    normalized = normalize_base_url(url)
    data = _read_config_json()
    data["forum_base_url"] = normalized
    config_path().write_text(json.dumps(data, indent=2), encoding="utf-8")
    return normalized
