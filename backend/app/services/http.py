"""Shared retry wrapper for outbound GET calls, built on tenacity.

All external calls (TMDB, torrentio, forum) are idempotent GETs, so retrying is
safe. We retry on transport errors, timeouts, and a small set of transient HTTP
statuses (429/5xx). Non-retryable statuses (e.g. 404) are returned unchanged for
the caller to handle. Policy is read from env via config.get_retry_settings().
"""
from __future__ import annotations

import logging

import httpx
from tenacity import (
    AsyncRetrying,
    retry_if_exception_type,
    stop_after_attempt,
    stop_after_delay,
    wait_exponential_jitter,
)

from app.config import get_retry_settings

log = logging.getLogger("app.http")

# Transient statuses worth retrying (rate limiting + upstream/server errors).
RETRY_STATUSES = frozenset({429, 500, 502, 503, 504})

# Exceptions that indicate a transient failure. HTTPStatusError is only raised by
# us (below) for RETRY_STATUSES, so it never triggers a retry on ordinary 4xx.
RETRYABLE_EXC = (httpx.TransportError, httpx.TimeoutException, httpx.HTTPStatusError)


async def get_with_retry(client: httpx.AsyncClient, url: str, **kwargs) -> httpx.Response:
    """GET `url` with retries. Returns the response (2xx or non-retryable status).

    On exhausted retries the last transient failure is re-raised (an
    httpx.HTTPStatusError for status-based failures, so callers/error formatting
    still see the upstream status code).
    """
    settings = get_retry_settings()

    def _before_sleep(retry_state) -> None:
        outcome = retry_state.outcome
        exc = outcome.exception() if outcome else None
        # Surface the underlying cause (e.g. DNS "nodename nor servname provided",
        # IPv6 "No route to host", TLS errors) so ConnectErrors are diagnosable.
        if exc is not None:
            cause = exc.__cause__
            detail = str(exc) or exc.__class__.__name__
            if cause is not None:
                detail = f"{exc.__class__.__name__}: {cause!r}"
            reason = f"{exc.__class__.__name__} ({detail})"
        else:
            reason = "?"
        log.warning(
            "Retry %d/%d for GET %s -> %s",
            retry_state.attempt_number,
            settings.max_attempts,
            url,
            reason,
        )

    async for attempt in AsyncRetrying(
        stop=stop_after_attempt(settings.max_attempts) | stop_after_delay(settings.deadline),
        wait=wait_exponential_jitter(
            initial=settings.backoff_base, max=settings.backoff_cap, jitter=settings.jitter
        ),
        retry=retry_if_exception_type(RETRYABLE_EXC),
        before_sleep=_before_sleep,
        reraise=True,
    ):
        with attempt:
            resp = await client.get(url, **kwargs)
            # Turn transient statuses into an exception so tenacity retries them;
            # other statuses (incl. non-retryable 4xx) are returned as-is.
            if resp.status_code in RETRY_STATUSES:
                resp.raise_for_status()
            return resp
