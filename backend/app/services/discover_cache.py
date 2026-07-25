"""In-process TTL cache for Discover (trending/popular/top-rated) TMDB responses.

These lists barely change minute to minute, so a plain dict cache keyed by
(category, media_type, tmdb_page) avoids re-hitting TMDB on every Discover expand
or "Load more" click. Key space is small and bounded, so no eviction policy is
needed. Lost on process restart — fine for this single-worker local tool.

A per-key asyncio.Lock serializes concurrent misses on the same key (e.g. two
"Load more" clicks that map to the same TMDB page arriving close together), so
only one of them actually calls TMDB and the other reuses its result instead of
both racing upstream.
"""
from __future__ import annotations

import asyncio
import time
from typing import Awaitable, Callable, TypeVar

from app.config import get_discover_cache_ttl_seconds

T = TypeVar("T")

_cache: dict[tuple[str, str, int], tuple[float, object]] = {}
_locks: dict[tuple[str, str, int], asyncio.Lock] = {}


def _fresh(key: tuple[str, str, int], ttl: float) -> tuple[bool, object]:
    cached = _cache.get(key)
    if cached is not None and (time.monotonic() - cached[0]) < ttl:
        return True, cached[1]
    return False, None


async def get_or_fetch(
    category: str, media_type: str, tmdb_page: int, fetch: Callable[[], Awaitable[T]]
) -> T:
    """Return the cached value for this key if still fresh, else await `fetch()`."""
    key = (category, media_type, tmdb_page)
    ttl = get_discover_cache_ttl_seconds()

    hit, value = _fresh(key, ttl)
    if hit:
        return value  # type: ignore[return-value]

    lock = _locks.setdefault(key, asyncio.Lock())
    async with lock:
        # Re-check: another coroutine may have populated the cache while we
        # were waiting for the lock.
        hit, value = _fresh(key, ttl)
        if hit:
            return value  # type: ignore[return-value]
        value = await fetch()
        _cache[key] = (time.monotonic(), value)
        return value
