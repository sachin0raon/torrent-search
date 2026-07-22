"""TMDB service: multi-search and external-ids lookup.

The API key is read server-side from the environment and never returned to clients.
"""
from __future__ import annotations

import httpx

from app.config import OUTBOUND_TIMEOUT_SECONDS, get_tmdb_api_key
from app.models import TitleResult
from app.services.http import get_with_retry

TMDB_BASE = "https://api.themoviedb.org/3"
_POSTER_BASE = "https://image.tmdb.org/t/p/w200"


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
    return results


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
