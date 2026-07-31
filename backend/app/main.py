"""FastAPI application entry point."""
from __future__ import annotations

import logging
import os
import sys
from contextlib import asynccontextmanager
from pathlib import Path

import httpx
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles

from app.config import (
    CORS_ORIGINS,
    OUTBOUND_TIMEOUT_SECONDS,
    REQUEST_HEADERS,
    get_forum_base_url,
    get_forum_probe_settings,
)
from app.routers import comet, discover, forum, meteor, search, settings, torrentio
from app.services.scheduler import create_scheduler


def configure_logging() -> logging.Logger:
    """Send application logs to stdout so they surface in `docker logs`.

    Attached to a dedicated "app" logger (propagate=False) so it coexists with
    uvicorn's own loggers without duplicating lines. Level via LOG_LEVEL env.
    """
    level = os.environ.get("LOG_LEVEL", "INFO").upper()
    logger = logging.getLogger("app")
    if not logger.handlers:
        handler = logging.StreamHandler(sys.stdout)
        handler.setFormatter(
            logging.Formatter("%(asctime)s %(levelname)s %(name)s: %(message)s")
        )
        logger.addHandler(handler)
    logger.setLevel(level)
    logger.propagate = False
    return logger


log = configure_logging()


# Serve the built React SPA when present (production/Docker). In dev the frontend
# is served by Vite instead, so this mount is simply absent. Configurable via
# STATIC_DIR; the "/" mount is added last so it never shadows the API routes.
def _static_dir() -> Path:
    override = os.environ.get("STATIC_DIR")
    return Path(override) if override else Path(__file__).resolve().parent.parent / "static"


_static = _static_dir()


@asynccontextmanager
async def lifespan(fastapi_app: FastAPI):
    base_url, source = get_forum_base_url()
    log.info(
        "Startup: forum_base_url=%s (source=%s), static_serving=%s",
        base_url or "<unset>",
        source,
        _static.is_dir(),
    )
    # One shared AsyncClient for the app lifetime (avoids per-request client churn).
    # For this low-traffic tool we DISABLE keep-alive reuse: idle pooled sockets
    # get closed by the server/CDN between our sporadic requests, and reusing a
    # dead one is what produces the intermittent ConnectError(EndOfStream()). A
    # fresh connection per request costs a little latency but removes that failure.
    # transport retries=2 also transparently re-establishes on connect-level errors.
    transport = httpx.AsyncHTTPTransport(
        retries=2,
        limits=httpx.Limits(max_connections=20, max_keepalive_connections=0),
    )
    async with httpx.AsyncClient(
        transport=transport,
        timeout=httpx.Timeout(OUTBOUND_TIMEOUT_SECONDS, connect=5.0),
        follow_redirects=True,
        headers=REQUEST_HEADERS,
    ) as client:
        fastapi_app.state.http = client
        probe = get_forum_probe_settings()
        scheduler = None
        if probe.enabled:
            scheduler = create_scheduler(client, probe.interval_minutes, probe.query)
            scheduler.start()
            log.info(
                "Forum base URL probe scheduled every %d min (query=%r)",
                probe.interval_minutes,
                probe.query,
            )
        else:
            log.info("Forum base URL probe disabled (FORUM_PROBE_ENABLED)")
        try:
            yield
        finally:
            if scheduler is not None:
                scheduler.shutdown(wait=False)
    log.info("Shutdown")


app = FastAPI(title="Torrent Search Aggregator", version="0.1.0", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=CORS_ORIGINS,
    allow_credentials=False,
    allow_methods=["GET", "PUT"],
    allow_headers=["*"],
)

# API routers are registered before the static mount so /api/* always wins.
app.include_router(search.router)
app.include_router(torrentio.router)
app.include_router(comet.router)
app.include_router(meteor.router)
app.include_router(forum.router)
app.include_router(settings.router)
app.include_router(discover.router)


@app.get("/api/health")
async def health():
    return {"status": "ok"}


if _static.is_dir():
    log.info("Serving static frontend from %s", _static)
    app.mount("/", StaticFiles(directory=str(_static), html=True), name="static")
else:
    log.info("No static frontend dir at %s (dev mode) — serving API only", _static)
