"""Torrentio service: build stream URL, fetch, parse titles, build magnets.

Torrentio addressing:
    movie                    -> /movie/{imdb_id}.json
    tv, whole series         -> /series/{imdb_id}.json
    tv, specific episode     -> /series/{imdb_id}:{season}:{episode}.json

The stream `title` is newline-separated, e.g.:
    "Ubuntu 22.04 desktop amd64\n👤 3945 💾 1.34 GB ⚙️ Bittorrent"
Line 1 is the display title; the metadata line carries seeders (👤), size (💾),
and provider (⚙️), each optional.
"""
from __future__ import annotations

import re

import httpx

from app.config import OUTBOUND_TIMEOUT_SECONDS, TORRENTIO_BASE
from app.models import TorrentioItem
from app.services.http import get_with_retry
from app.utils.magnet import build_magnet

_SEEDERS_RE = re.compile(r"👤\s*([\d,]+)")
_SIZE_RE = re.compile(r"💾\s*([^⚙️👤]+)")
_PROVIDER_RE = re.compile(r"⚙️\s*([^\n]+)")


def build_stream_url(
    imdb_id: str,
    media_type: str,
    season: int | None = None,
    episode: int | None = None,
) -> str:
    if media_type == "movie":
        return f"{TORRENTIO_BASE}/movie/{imdb_id}.json"
    # tv / series
    if season is not None and episode is not None:
        return f"{TORRENTIO_BASE}/series/{imdb_id}:{season}:{episode}.json"
    return f"{TORRENTIO_BASE}/series/{imdb_id}.json"


def parse_stream_title(raw_title: str) -> dict:
    """Extract display title + optional seeders/size/provider from a raw title."""
    lines = (raw_title or "").split("\n")
    display_title = lines[0].strip() if lines else ""

    seeders = None
    m = _SEEDERS_RE.search(raw_title or "")
    if m:
        try:
            seeders = int(m.group(1).replace(",", ""))
        except ValueError:
            seeders = None

    size = None
    m = _SIZE_RE.search(raw_title or "")
    if m:
        size = m.group(1).strip() or None

    source = None
    m = _PROVIDER_RE.search(raw_title or "")
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
    for s in streams or []:
        info_hash = s.get("infoHash")
        if not info_hash:
            continue
        parsed = parse_stream_title(s.get("title", ""))
        magnet = build_magnet(parsed["title"], info_hash, s.get("sources"))
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
    client = client or httpx.AsyncClient(timeout=OUTBOUND_TIMEOUT_SECONDS)
    try:
        resp = await get_with_retry(client, url)
        resp.raise_for_status()
        data = resp.json()
    finally:
        if owns_client:
            await client.aclose()
    return streams_to_items(data.get("streams", []))
