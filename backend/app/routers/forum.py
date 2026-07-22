"""Stage 4: lazily fetch & parse a single forum topic page for file/magnet links."""
from __future__ import annotations

import logging

from fastapi import APIRouter, HTTPException, Query, Request

from app.models import TopicResponse
from app.services import forum

log = logging.getLogger("app.forum")
router = APIRouter(prefix="/api/forum", tags=["forum"])


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
