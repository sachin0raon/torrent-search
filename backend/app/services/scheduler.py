"""Background scheduler: periodically probe the forum base URL for domain redirects.

The forum occasionally moves to a new domain, issuing an HTTP redirect from the
old one. `forum.search()` already follows redirects and reports the origin it
actually reached; this job runs that search on a timer and persists any changed
origin — so a moved domain is picked up even while the app is idle, not only on
the next user search.

The probe reuses the search API (not a bare redirect follow) so it only ever
persists an origin that returned a valid forum search response, never a parking
or CDN-challenge page.

Note: this is an in-process scheduler — run a single uvicorn worker. With
multiple workers each would run its own probe (redundant, but harmless).
"""
from __future__ import annotations

import logging
from datetime import datetime

import httpx
from apscheduler.schedulers.asyncio import AsyncIOScheduler

from app.config import get_forum_base_url
from app.services import forum

log = logging.getLogger("app.scheduler")

_JOB_ID = "forum_base_probe"


async def probe_forum_base(client: httpx.AsyncClient, query: str) -> None:
    """Run one probe: search the configured base and persist a redirected origin."""
    base_url, _ = get_forum_base_url()
    if not base_url:
        log.debug("forum base probe skipped: base URL not configured")
        return
    try:
        result = await forum.search(base_url, query, client=client)
    except Exception as e:
        # A failed probe (network error, old domain gone dark) must never crash
        # the scheduler — just log and wait for the next run.
        log.info("forum base probe failed (base=%s): %s", base_url, e)
        return
    await forum.maybe_persist_new_base(base_url, result.resolved_base_url)


def create_scheduler(
    client: httpx.AsyncClient, interval_minutes: int, query: str
) -> AsyncIOScheduler:
    """Build an AsyncIOScheduler with the forum-probe job registered.

    The job runs once shortly after startup, then every `interval_minutes`.
    Caller is responsible for start()/shutdown().
    """
    scheduler = AsyncIOScheduler()
    scheduler.add_job(
        probe_forum_base,
        "interval",
        minutes=interval_minutes,
        kwargs={"client": client, "query": query},
        id=_JOB_ID,
        next_run_time=datetime.now(),  # probe once soon after startup
        max_instances=1,  # never overlap probes
        coalesce=True,  # collapse missed runs into one
    )
    return scheduler
