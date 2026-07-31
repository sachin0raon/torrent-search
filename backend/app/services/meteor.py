"""Meteor service: build stream URL, fetch, parse titles, build magnets.

Meteor addressing mirrors Torrentio:
    movie                    -> /movie/{imdb_id}.json
    tv, whole series         -> /series/{imdb_id}.json
    tv, specific episode     -> /series/{imdb_id}:{season}:{episode}.json

Like Comet, Meteor packs its title/metadata into the `description` field (not
`title`), first line prefixed "📄 ", e.g.:
    "📄 Movie.2026.1080p.WEB.h264\n📺 1080p | webdl | h264\n💾 5.45 GiB"
Metadata: size (💾) and quality/resolution (📺, stored in the shared `source`
slot — there's no dedicated quality field on the shared item shape). Seeders
(👤) are absent from most sample responses (Meteor's configured resultFormat
often omits them) but are still matched opportunistically in case a different
title/config surfaces one. Unlike Comet, Meteor's config already requests
seeders-first sorting upstream, so no local sort is applied here.
"""
from __future__ import annotations

import re

import httpx

from app.config import METEOR_BASE, OUTBOUND_TIMEOUT_SECONDS, REQUEST_HEADERS
from app.models import TorrentioItem
from app.services.http import get_with_retry
from app.utils.magnet import build_magnet

_SEEDERS_RE = re.compile(r"👤\s*([\d,]+)")
_SIZE_RE = re.compile(r"💾\s*([^📺👤🔊]+)")
_QUALITY_RE = re.compile(r"📺\s*([^\n]+)")


def build_stream_url(
    imdb_id: str,
    media_type: str,
    season: int | None = None,
    episode: int | None = None,
) -> str:
    if media_type == "movie":
        return f"{METEOR_BASE}/movie/{imdb_id}.json"
    if season is not None and episode is not None:
        return f"{METEOR_BASE}/series/{imdb_id}:{season}:{episode}.json"
    return f"{METEOR_BASE}/series/{imdb_id}.json"


def parse_stream_title(raw_description: str) -> dict:
    """Extract display title + optional seeders/size/quality from a raw description."""
    lines = (raw_description or "").split("\n")
    display_title = lines[0].strip() if lines else ""
    if display_title.startswith("📄"):
        display_title = display_title.lstrip("📄").strip()

    seeders = None
    m = _SEEDERS_RE.search(raw_description or "")
    if m:
        try:
            seeders = int(m.group(1).replace(",", ""))
        except ValueError:
            seeders = None

    size = None
    m = _SIZE_RE.search(raw_description or "")
    if m:
        size = m.group(1).strip() or None

    source = None
    m = _QUALITY_RE.search(raw_description or "")
    if m:
        source = m.group(1).strip() or None

    return {
        "title": display_title,
        "seeders": seeders,
        "size": size,
        "source": source,
    }


def streams_to_items(streams: list[dict]) -> list[TorrentioItem]:
    items: list[TorrentioItem] = []
    seen_magnets: set[str] = set()
    for s in streams or []:
        info_hash = s.get("infoHash")
        if not info_hash:
            continue
        parsed = parse_stream_title(s.get("description", ""))
        magnet = build_magnet(parsed["title"], info_hash, s.get("sources"))
        if magnet in seen_magnets:
            continue
        seen_magnets.add(magnet)
        items.append(
            TorrentioItem(
                title=parsed["title"] or (s.get("behaviorHints", {}) or {}).get("filename", ""),
                seeders=parsed["seeders"],
                size=parsed["size"],
                source=parsed["source"],
                magnet=magnet,
            )
        )
    return items


async def fetch_streams(
    imdb_id: str,
    media_type: str,
    season: int | None = None,
    episode: int | None = None,
    client: httpx.AsyncClient | None = None,
) -> list[TorrentioItem]:
    url = build_stream_url(imdb_id, media_type, season, episode)
    owns_client = client is None
    # Mirror the shared app-lifetime client's headers/redirect behavior (see
    # main.py's lifespan) so a standalone call (script, REPL, future caller)
    # doesn't silently lose the browser User-Agent/redirect-following that
    # keeps Meteor's 302 (seen from a VPS) from turning into a broken fetch.
    client = client or httpx.AsyncClient(
        timeout=OUTBOUND_TIMEOUT_SECONDS, follow_redirects=True, headers=REQUEST_HEADERS
    )
    try:
        resp = await get_with_retry(client, url)
        resp.raise_for_status()
        data = resp.json()
    finally:
        if owns_client:
            await client.aclose()
    return streams_to_items(data.get("streams", []))
