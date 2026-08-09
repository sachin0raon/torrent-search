"""Settings: read/update the runtime-overridable forum base URL."""
from __future__ import annotations

from fastapi import APIRouter, HTTPException

from app.config import ConfigError, get_file_browser_url, get_forum_base_url, set_forum_base_url
from app.models import ConfigResponse, ConfigUpdate

router = APIRouter(prefix="/api", tags=["settings"])


@router.get("/config", response_model=ConfigResponse)
async def read_config():
    url, source = get_forum_base_url()
    return ConfigResponse(
        forum_base_url=url,
        source=source,
        file_browser_url=get_file_browser_url(),
    )


@router.put("/config", response_model=ConfigResponse)
async def update_config(payload: ConfigUpdate):
    try:
        saved = set_forum_base_url(payload.forum_base_url)
    except (ConfigError, ValueError) as e:
        raise HTTPException(status_code=400, detail=str(e))
    return ConfigResponse(forum_base_url=saved, source="config")
