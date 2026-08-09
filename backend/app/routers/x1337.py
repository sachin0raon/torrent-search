"""1337x router: search and magnet endpoints."""
from __future__ import annotations

import logging

from fastapi import APIRouter, HTTPException, Query, Request

from app.models import X1337MagnetResponse, X1337SearchResponse
from app.services import x1337
from app.services.http import error_message

log = logging.getLogger("app.x1337")
router = APIRouter(prefix="/api/1337x", tags=["1337x"])


@router.get("/search", response_model=X1337SearchResponse)
async def search(request: Request, q: str = Query(..., min_length=1)):
    try:
        items = await x1337.search_1337x(q, client=request.app.state.http)
        log.info("1337x search %r ok: %d items", q, len(items))
        return X1337SearchResponse(ok=True, items=items)
    except Exception as e:
        log.warning("1337x search %r failed: %s", q, error_message(e))
        return X1337SearchResponse(ok=False, error=error_message(e), items=[])


@router.get("/magnet", response_model=X1337MagnetResponse)
async def magnet(request: Request, path: str = Query(..., min_length=1)):
    try:
        magnet_url = await x1337.fetch_1337x_magnet(path, client=request.app.state.http)
        if not magnet_url:
            raise HTTPException(status_code=404, detail="Magnet link not found on detail page")
        log.info("1337x magnet fetch %r ok", path)
        return X1337MagnetResponse(magnet=magnet_url)
    except HTTPException:
        raise
    except Exception as e:
        log.warning("1337x magnet fetch failed path=%s: %s", path, e)
        raise HTTPException(status_code=502, detail=f"Failed to fetch magnet: {e}")
