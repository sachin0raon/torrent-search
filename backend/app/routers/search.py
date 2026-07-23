"""Stage 1 & 2: TMDB title search and imdb_id resolution."""
from __future__ import annotations

import logging

from fastapi import APIRouter, HTTPException, Query, Request

from app.config import ConfigError
from app.models import ExternalIdsResponse, SearchResponse, TvSeasonsResponse
from app.services import tmdb

log = logging.getLogger("app.search")
router = APIRouter(prefix="/api", tags=["search"])


@router.get("/search", response_model=SearchResponse)
async def search(request: Request, query: str = Query(..., min_length=1)):
    try:
        results = await tmdb.search_multi(query, client=request.app.state.http)
    except ConfigError as e:
        log.error("TMDB search misconfigured: %s", e)
        raise HTTPException(status_code=500, detail=str(e))
    except Exception as e:  # upstream/network failure
        log.exception("TMDB search failed for query=%r", query)
        raise HTTPException(status_code=502, detail=f"TMDB search failed: {e}")
    log.info("Search query=%r -> %d results", query, len(results))
    return SearchResponse(results=results)


@router.get("/external-ids", response_model=ExternalIdsResponse)
async def external_ids(
    request: Request,
    media_type: str = Query(..., pattern="^(movie|tv)$"),
    tmdb_id: int = Query(...),
):
    try:
        imdb_id = await tmdb.external_ids(media_type, tmdb_id, client=request.app.state.http)
    except ConfigError as e:
        log.error("TMDB external_ids misconfigured: %s", e)
        raise HTTPException(status_code=500, detail=str(e))
    except Exception as e:
        log.exception("TMDB external_ids failed for %s/%s", media_type, tmdb_id)
        raise HTTPException(status_code=502, detail=f"TMDB external_ids failed: {e}")
    log.info("external_ids %s/%s -> imdb_id=%s", media_type, tmdb_id, imdb_id)
    return ExternalIdsResponse(imdb_id=imdb_id)


@router.get("/tv-seasons", response_model=TvSeasonsResponse)
async def tv_seasons(request: Request, tmdb_id: int = Query(...)):
    try:
        seasons = await tmdb.tv_seasons(tmdb_id, client=request.app.state.http)
    except ConfigError as e:
        log.error("TMDB tv_seasons misconfigured: %s", e)
        raise HTTPException(status_code=500, detail=str(e))
    except Exception as e:
        log.exception("TMDB tv_seasons failed for %s", tmdb_id)
        raise HTTPException(status_code=502, detail=f"TMDB tv_seasons failed: {e}")
    log.info("tv_seasons %s -> %d seasons", tmdb_id, len(seasons))
    return TvSeasonsResponse(seasons=seasons)
