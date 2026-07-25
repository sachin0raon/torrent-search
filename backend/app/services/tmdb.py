"""TMDB service: multi-search and external-ids lookup.

The API key is read server-side from the environment and never returned to clients.
"""
from __future__ import annotations

import httpx

from app.config import OUTBOUND_TIMEOUT_SECONDS, get_tmdb_api_key
from app.models import TitleResult, TvSeason
from app.services.http import get_with_retry

TMDB_BASE = "https://api.themoviedb.org/3"
_POSTER_BASE = "https://image.tmdb.org/t/p/w200"


def _dedup(items: list[TitleResult]) -> list[TitleResult]:
    """Drop later duplicates by (media_type, tmdb_id), keeping first-seen order.

    TMDB occasionally returns the same title twice in one response (seen on
    search/multi and the discover endpoints); duplicates would otherwise reach
    the frontend as two list items sharing one React key.
    """
    seen: set[tuple[str, int]] = set()
    out = []
    for tr in items:
        key = (tr.media_type, tr.tmdb_id)
        if key in seen:
            continue
        seen.add(key)
        out.append(tr)
    return out


def _to_title_result(item: dict) -> TitleResult | None:
    media_type = item.get("media_type")
    if media_type not in ("movie", "tv"):
        return None
    # Movies use title/release_date; TV uses name/first_air_date.
    title = item.get("title") or item.get("name") or ""
    date = item.get("release_date") or item.get("first_air_date") or ""
    year = date[:4] if date else None
    poster_path = item.get("poster_path")
    return TitleResult(
        tmdb_id=item["id"],
        media_type=media_type,
        title=title,
        year=year,
        poster_url=f"{_POSTER_BASE}{poster_path}" if poster_path else None,
        overview=item.get("overview") or None,
    )


async def search_multi(query: str, client: httpx.AsyncClient | None = None) -> list[TitleResult]:
    url = f"{TMDB_BASE}/search/multi"
    params = {"query": query, "api_key": get_tmdb_api_key()}
    owns_client = client is None
    client = client or httpx.AsyncClient(timeout=OUTBOUND_TIMEOUT_SECONDS)
    try:
        resp = await get_with_retry(client, url, params=params)
        resp.raise_for_status()
        data = resp.json()
    finally:
        if owns_client:
            await client.aclose()
    results = []
    for item in data.get("results", []):
        tr = _to_title_result(item)
        if tr is not None:
            results.append(tr)
    return _dedup(results)


async def external_ids(
    media_type: str, tmdb_id: int, client: httpx.AsyncClient | None = None
) -> str | None:
    if media_type not in ("movie", "tv"):
        raise ValueError("media_type must be 'movie' or 'tv'")
    url = f"{TMDB_BASE}/{media_type}/{tmdb_id}/external_ids"
    params = {"api_key": get_tmdb_api_key()}
    owns_client = client is None
    client = client or httpx.AsyncClient(timeout=OUTBOUND_TIMEOUT_SECONDS)
    try:
        resp = await get_with_retry(client, url, params=params)
        resp.raise_for_status()
        data = resp.json()
    finally:
        if owns_client:
            await client.aclose()
    imdb_id = (data.get("imdb_id") or "").strip()
    return imdb_id or None


async def _discover_list(
    url: str, media_type: str, page: int, client: httpx.AsyncClient | None = None
) -> list[TitleResult]:
    """Shared fetch for the Discover endpoints below: same request/parse shape,
    but each item is missing `media_type` (the TMDB endpoint is type-specific,
    unlike search/multi), so it's injected before parsing."""
    params = {"api_key": get_tmdb_api_key(), "page": page}
    owns_client = client is None
    client = client or httpx.AsyncClient(timeout=OUTBOUND_TIMEOUT_SECONDS)
    try:
        resp = await get_with_retry(client, url, params=params)
        resp.raise_for_status()
        data = resp.json()
    finally:
        if owns_client:
            await client.aclose()
    results = []
    for item in data.get("results", []):
        item = {**item, "media_type": media_type}
        tr = _to_title_result(item)
        if tr is not None:
            results.append(tr)
    return _dedup(results)


async def trending(
    media_type: str, page: int = 1, client: httpx.AsyncClient | None = None
) -> list[TitleResult]:
    url = f"{TMDB_BASE}/trending/{media_type}/week"
    return await _discover_list(url, media_type, page, client)


async def popular(
    media_type: str, page: int = 1, client: httpx.AsyncClient | None = None
) -> list[TitleResult]:
    url = f"{TMDB_BASE}/{media_type}/popular"
    return await _discover_list(url, media_type, page, client)


async def top_rated(
    media_type: str, page: int = 1, client: httpx.AsyncClient | None = None
) -> list[TitleResult]:
    url = f"{TMDB_BASE}/{media_type}/top_rated"
    return await _discover_list(url, media_type, page, client)


async def tv_seasons(tmdb_id: int, client: httpx.AsyncClient | None = None) -> list[TvSeason]:
    """Return the seasons of a TV show (each with its episode_count).

    A single /tv/{id} call includes the `seasons` array with episode_count per
    season, so no per-season fetch is needed. Seasons with no episodes are dropped.
    """
    url = f"{TMDB_BASE}/tv/{tmdb_id}"
    params = {"api_key": get_tmdb_api_key()}
    owns_client = client is None
    client = client or httpx.AsyncClient(timeout=OUTBOUND_TIMEOUT_SECONDS)
    try:
        resp = await get_with_retry(client, url, params=params)
        resp.raise_for_status()
        data = resp.json()
    finally:
        if owns_client:
            await client.aclose()

    seasons: list[TvSeason] = []
    for s in data.get("seasons", []):
        count = s.get("episode_count") or 0
        number = s.get("season_number")
        if count <= 0 or number is None:
            continue
        seasons.append(TvSeason(season_number=number, name=s.get("name"), episode_count=count))
    seasons.sort(key=lambda s: s.season_number)
    return seasons
