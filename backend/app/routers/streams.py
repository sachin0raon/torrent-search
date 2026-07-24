"""Stage 3: parallel fan-out to torrentio + forum search with partial-failure handling."""
from __future__ import annotations

import asyncio
import logging

import httpx
from fastapi import APIRouter, Query, Request

from app.config import get_forum_base_url
from app.models import SourceResult, StreamsResponse
from app.services import forum, torrentio

log = logging.getLogger("app.streams")
router = APIRouter(prefix="/api", tags=["streams"])


def _error_message(exc: Exception) -> str:
    if isinstance(exc, httpx.TimeoutException):
        return "timed out"
    if isinstance(exc, httpx.HTTPStatusError):
        return f"upstream returned {exc.response.status_code}"
    return str(exc) or exc.__class__.__name__


async def _torrentio_source(
    imdb_id: str | None,
    media_type: str,
    season: int | None,
    episode: int | None,
    client: httpx.AsyncClient,
) -> SourceResult:
    if not imdb_id:
        log.info("torrentio skipped: no imdb_id for this title")
        return SourceResult(ok=False, error="No IMDb ID available for this title", items=[])
    try:
        items = await torrentio.fetch_streams(imdb_id, media_type, season, episode, client=client)
        log.info("torrentio %s ok: %d streams", imdb_id, len(items))
        return SourceResult(ok=True, items=items)
    except Exception as e:
        log.warning("torrentio %s failed: %s", imdb_id, _error_message(e))
        return SourceResult(ok=False, error=_error_message(e), items=[])


async def _forum_source(
    raw_query: str, client: httpx.AsyncClient
) -> tuple[SourceResult, str | None]:
    """Returns (result, updated_base_url). updated_base_url is set when a forum
    domain redirect was detected and the new base URL was persisted."""
    base_url, _ = get_forum_base_url()
    if not base_url:
        log.warning("forum search skipped: base URL not configured")
        return SourceResult(ok=False, error="Forum base URL is not configured", items=[]), None
    try:
        result = await forum.search(base_url, raw_query, client=client)
        updated = await forum.maybe_persist_new_base(base_url, result.resolved_base_url)
        log.info("forum search %r ok: %d results", raw_query, len(result.items))
        return SourceResult(ok=True, items=result.items), updated
    except Exception as e:
        log.warning("forum search %r failed: %s", raw_query, _error_message(e))
        return SourceResult(ok=False, error=_error_message(e), items=[]), None


@router.get("/streams", response_model=StreamsResponse)
async def streams(
    request: Request,
    imdb_id: str | None = Query(None),
    media_type: str = Query(..., pattern="^(movie|tv)$"),
    raw_query: str = Query(..., min_length=1),
    season: int | None = Query(None),
    episode: int | None = Query(None),
):
    # Shared app-lifetime client; run both sources concurrently.
    client = request.app.state.http
    torrentio_res, forum_pair = await asyncio.gather(
        _torrentio_source(imdb_id, media_type, season, episode, client),
        _forum_source(raw_query, client),
    )
    forum_res, forum_updated = forum_pair
    return StreamsResponse(
        torrentio=torrentio_res, forum=forum_res, forum_base_updated=forum_updated
    )
