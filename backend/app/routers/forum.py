"""Stage 3 (forum half): independent forum search endpoint, own retry.
Stage 4: lazily fetch & parse a single forum topic page for file/magnet links.
"""
from __future__ import annotations

import asyncio
import logging

from fastapi import APIRouter, HTTPException, Query, Request

from app.config import get_forum_base_url
from app.models import ForumSearchResponse, TopicResponse
from app.services import forum
from app.services.http import error_message

log = logging.getLogger("app.forum")
router = APIRouter(prefix="/api/forum", tags=["forum"])


@router.get("/search", response_model=ForumSearchResponse)
async def search(request: Request, raw_query: str = Query(..., min_length=1)):
    # get_forum_base_url() does a blocking config.json read; offload it so it
    # never stalls the event loop, matching maybe_persist_new_base's write path.
    base_url, _ = await asyncio.to_thread(get_forum_base_url)
    if not base_url:
        log.warning("forum search skipped: base URL not configured")
        return ForumSearchResponse(
            ok=False, error="Forum base URL is not configured", items=[]
        )
    try:
        result = await forum.search(base_url, raw_query, client=request.app.state.http)
        updated = await forum.maybe_persist_new_base(base_url, result.resolved_base_url)
        log.info("forum search %r ok: %d results", raw_query, len(result.items))
        return ForumSearchResponse(ok=True, items=result.items, forum_base_updated=updated)
    except Exception as e:
        log.warning("forum search %r failed: %s", raw_query, error_message(e))
        return ForumSearchResponse(ok=False, error=error_message(e), items=[])


@router.get("/topic", response_model=TopicResponse)
async def topic(request: Request, url: str = Query(..., min_length=1)):
    if not url.startswith(("http://", "https://")):
        raise HTTPException(status_code=400, detail="url must be an http(s) URL")
    try:
        links = await forum.fetch_topic(url, client=request.app.state.http)
    except Exception as e:
        log.warning("forum topic fetch failed url=%s: %s", url, e)
        raise HTTPException(status_code=502, detail=f"Failed to fetch topic: {e}")
    log.info("forum topic %s -> %d links", url, len(links))
    return TopicResponse(links=links)
