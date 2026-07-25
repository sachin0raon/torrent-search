"""Discover: trending / popular / top-rated rails (movie + tv), TMDB-backed.

UI pages are 10 items; TMDB's native page size is 20. Two consecutive UI pages
share one TMDB page (and therefore one cache entry / one TMDB call).
"""
from __future__ import annotations

import logging

from fastapi import APIRouter, HTTPException, Query, Request

from app.config import ConfigError
from app.models import SearchResponse
from app.services import discover_cache, tmdb

log = logging.getLogger("app.discover")
router = APIRouter(prefix="/api", tags=["discover"])

_FETCHERS = {
    "trending": tmdb.trending,
    "popular": tmdb.popular,
    "top_rated": tmdb.top_rated,
}

_UI_PAGE_SIZE = 10  # TMDB's native page size is 20 -> two UI pages per TMDB page


@router.get("/discover", response_model=SearchResponse)
async def discover(
    request: Request,
    category: str = Query(..., pattern="^(trending|popular|top_rated)$"),
    media_type: str = Query(..., pattern="^(movie|tv)$"),
    page: int = Query(1, ge=1),
):
    fetch = _FETCHERS[category]
    tmdb_page = (page - 1) // 2 + 1
    offset_in_tmdb_page = ((page - 1) % 2) * _UI_PAGE_SIZE

    async def _fetch():
        return await fetch(media_type, tmdb_page, client=request.app.state.http)

    try:
        results = await discover_cache.get_or_fetch(category, media_type, tmdb_page, _fetch)
    except ConfigError as e:
        log.error("Discover misconfigured: %s", e)
        raise HTTPException(status_code=500, detail=str(e))
    except Exception as e:
        log.exception("Discover failed for category=%s media_type=%s page=%s", category, media_type, page)
        raise HTTPException(status_code=502, detail=f"Discover failed: {e}")

    page_slice = results[offset_in_tmdb_page : offset_in_tmdb_page + _UI_PAGE_SIZE]
    log.info(
        "Discover %s/%s page=%d -> %d results (tmdb_page=%d)",
        category, media_type, page, len(page_slice), tmdb_page,
    )
    return SearchResponse(results=page_slice)
