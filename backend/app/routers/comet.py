"""Independent Comet source endpoint (own retry, no bundling with other sources)."""
from __future__ import annotations

import logging

import httpx
from fastapi import APIRouter, Query, Request

from app.models import SourceResult
from app.services import comet
from app.services.http import error_message

log = logging.getLogger("app.comet")
router = APIRouter(prefix="/api", tags=["comet"])


@router.get("/comet", response_model=SourceResult)
async def get_comet(
    request: Request,
    imdb_id: str | None = Query(None),
    media_type: str = Query(..., pattern="^(movie|tv)$"),
    season: int | None = Query(None),
    episode: int | None = Query(None),
):
    if not imdb_id:
        log.info("comet skipped: no imdb_id for this title")
        return SourceResult(ok=False, error="No IMDb ID available for this title", items=[])
    client: httpx.AsyncClient = request.app.state.http
    try:
        items = await comet.fetch_streams(imdb_id, media_type, season, episode, client=client)
        log.info("comet %s ok: %d streams", imdb_id, len(items))
        return SourceResult(ok=True, items=items)
    except Exception as e:
        log.warning("comet %s failed: %s", imdb_id, error_message(e))
        return SourceResult(ok=False, error=error_message(e), items=[])
