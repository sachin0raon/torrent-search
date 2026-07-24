"""Forum service: search API, topic-URL slug building, and topic HTML parsing.

Flow:
    1. GET {base}/search/api/search.php?q=&priority=1&sort=title_asc&page=1&per_page=25
       -> results: [{tid, title}]
    2. Build a topic URL per result: {base}/index.php?/topic/{tid}-{slug}/
    3. On demand, fetch the topic HTML and pair file links with magnet links.

HTML pairing (per design):
    - file anchors: class contains "ipsAttachLink" or "ipsAttachLink_block"
      -> filename = anchor text, file_url = href
    - magnet anchors: href starts with "magnet:"
    - each file anchor is paired with the NEXT magnet anchor in document order.
      Unpaired file -> magnet None; stray magnet -> its own row (filename None).
"""
from __future__ import annotations

import asyncio
import logging
import re
from typing import NamedTuple
from urllib.parse import parse_qs, urlsplit

import httpx
from bs4 import BeautifulSoup

from app.config import ConfigError, OUTBOUND_TIMEOUT_SECONDS, set_forum_base_url
from app.models import ForumSearchItem, TopicLink
from app.services.http import get_with_retry

log = logging.getLogger("app.forum")

# Serializes config.json read-modify-write between a user request and the
# background probe so a concurrent redirect-persist can't lose an update.
_persist_lock = asyncio.Lock()


async def maybe_persist_new_base(current: str, resolved: str) -> str | None:
    """If the reached origin differs from the configured base, persist it.

    Only call after a valid search response, so `resolved` is a real forum
    origin (not a parking/challenge page). Returns the saved URL, or None.
    """
    if not resolved or resolved.rstrip("/") == current.rstrip("/"):
        return None
    async with _persist_lock:
        try:
            # config.json write is blocking file I/O; offload it so the persist
            # (which runs under the lock) never stalls the event loop.
            saved = await asyncio.to_thread(set_forum_base_url, resolved)
        except ConfigError as e:
            log.warning("Detected new forum URL %s but it failed validation: %s", resolved, e)
            return None
    log.info("Forum base URL auto-updated after redirect: %s -> %s", current, saved)
    return saved


class ForumSearchResult(NamedTuple):
    items: list[ForumSearchItem]
    # Origin (scheme://host) actually reached after any redirects — lets callers
    # detect and persist a changed forum domain.
    resolved_base_url: str


def _origin(url: str) -> str:
    parts = urlsplit(url)
    return f"{parts.scheme}://{parts.netloc}" if parts.scheme and parts.netloc else ""

_SLUG_STRIP_RE = re.compile(r"[^a-z0-9\s-]")
_SLUG_SPACE_RE = re.compile(r"\s+")
_SLUG_DASH_RE = re.compile(r"-+")

SLUG_MAX_LEN = 80


def build_slug(title: str) -> str:
    """lowercase -> drop non [a-z0-9 space hyphen] -> spaces to hyphens ->
    collapse repeats -> truncate to <=80 chars. Trailing hyphen is allowed."""
    slug = (title or "").lower()
    slug = _SLUG_STRIP_RE.sub("", slug)
    slug = _SLUG_SPACE_RE.sub("-", slug.strip())
    slug = _SLUG_DASH_RE.sub("-", slug)
    return slug[:SLUG_MAX_LEN]


def build_topic_url(base_url: str, tid: int, title: str) -> str:
    return f"{base_url.rstrip('/')}/index.php?/topic/{tid}-{build_slug(title)}/"


# Attachment-link classes for torrent files (per spec: ipsAttachLink / _block).
_ATTACH_CLASSES = frozenset({"ipsAttachLink", "ipsAttachLink_block"})


def magnet_display_name(magnet: str) -> str | None:
    """Extract a human-readable name from a magnet URI's `dn` parameter.

    Many forum posts render only the magnet link (the .torrent attachment link
    is added client-side by JS and isn't in the server HTML). The magnet's `dn`
    carries the release name, so we use it to label otherwise-unnamed magnets.
    """
    query = magnet.split("?", 1)[1] if "?" in magnet else ""
    values = parse_qs(query).get("dn")  # parse_qs already url-decodes values
    if values and values[0].strip():
        return values[0].strip()
    return None


def _is_torrent_file_link(tag) -> bool:
    """True if the anchor is a torrent-file attachment (not an image/other file).

    IPS posts also render image attachments (screenshots) with the base
    `ipsAttachLink` class, which must NOT be treated as torrent files — otherwise
    they create junk rows and steal magnets from real torrent links during
    pairing. The reliable discriminator is `data-fileext="torrent"`; when absent
    we fall back to a `.torrent` hint in the link text/href, and always exclude
    the image variant.
    """
    classes = tag.get("class") or []
    if not any(c in _ATTACH_CLASSES for c in classes):
        return False
    if any("ipsAttachLink_image" in c for c in classes):
        return False

    ext = (tag.get("data-fileext") or "").strip().lower()
    if ext:
        return ext == "torrent"

    text = tag.get_text(strip=True).lower()
    href = (tag.get("href") or "").lower()
    return text.endswith(".torrent") or ".torrent" in href


def parse_topic_html(html: str) -> list[TopicLink]:
    """Pair each torrent-file link with its adjacent magnet link.

    Real forum layouts place the magnet either immediately AFTER the file link
    (the common inline case) or immediately BEFORE it (common for the "block"
    styled attachments). We therefore check the next neighbour first, then the
    previous one. Each magnet is claimed once; leftover magnets become their own
    rows, and file links with no adjacent magnet get magnet=None.
    """
    soup = BeautifulSoup(html, "lxml")

    # Walk all anchors once, tagging each as 'file', 'magnet', or ignore.
    seq: list[tuple[str, dict]] = []
    for a in soup.find_all("a"):
        href = a.get("href", "") or ""
        if href.startswith("magnet:"):
            seq.append(("magnet", {"magnet": href}))
        elif _is_torrent_file_link(a):
            seq.append(
                ("file", {"filename": a.get_text(strip=True) or None, "file_url": href or None})
            )

    n = len(seq)
    claimed: set[int] = set()  # indices of magnets already paired to a file
    links: list[TopicLink] = []

    for i, (kind, data) in enumerate(seq):
        if kind != "file":
            continue
        magnet = None
        if i + 1 < n and seq[i + 1][0] == "magnet" and (i + 1) not in claimed:
            magnet = seq[i + 1][1]["magnet"]
            claimed.add(i + 1)
        elif i - 1 >= 0 and seq[i - 1][0] == "magnet" and (i - 1) not in claimed:
            magnet = seq[i - 1][1]["magnet"]
            claimed.add(i - 1)
        # Fall back to the magnet's `dn` name if the file link had no text.
        filename = data["filename"] or (magnet_display_name(magnet) if magnet else None)
        links.append(
            TopicLink(filename=filename, file_url=data["file_url"], magnet=magnet)
        )

    # Any magnet not paired with a file becomes its own row, named from its `dn`.
    for i, (kind, data) in enumerate(seq):
        if kind == "magnet" and i not in claimed:
            links.append(
                TopicLink(
                    filename=magnet_display_name(data["magnet"]),
                    file_url=None,
                    magnet=data["magnet"],
                )
            )

    # Debug view of the exact file/magnet interleaving so mispairings on a real
    # page can be diagnosed without guessing (enable with LOG_LEVEL=debug).
    if log.isEnabledFor(logging.DEBUG):
        order = [
            ("F:" + (d["filename"][:28] if d.get("filename") else "?"))
            if k == "file"
            else ("M:" + d["magnet"][:24])
            for k, d in seq
        ]
        log.debug("topic anchor order (%d): %s", len(seq), order)
        log.debug(
            "topic parsed: %d rows, %d files, %d magnets, %d magnet-only(unnamed)",
            len(links),
            sum(1 for k, _ in seq if k == "file"),
            sum(1 for k, _ in seq if k == "magnet"),
            sum(1 for l in links if l.filename is None),
        )

    return links


def _results_to_items(base_url: str, results: list[dict]) -> list[ForumSearchItem]:
    items: list[ForumSearchItem] = []
    for r in results or []:
        tid = r.get("tid")
        title = r.get("title", "")
        if tid is None:
            continue
        items.append(
            ForumSearchItem(tid=tid, title=title, topic_url=build_topic_url(base_url, tid, title))
        )
    return items


async def search(
    base_url: str, query: str, client: httpx.AsyncClient | None = None
) -> ForumSearchResult:
    url = f"{base_url.rstrip('/')}/search/api/search.php"
    params = {
        "q": query,
        "priority": 1,
        "sort": "title_asc",
        "page": 1,
        "per_page": 25,
    }
    owns_client = client is None
    client = client or httpx.AsyncClient(
        timeout=OUTBOUND_TIMEOUT_SECONDS, follow_redirects=True
    )
    try:
        resp = await get_with_retry(client, url, params=params)
        resp.raise_for_status()
        data = resp.json()  # valid JSON here confirms we reached the real forum
        # Origin actually reached (after any 3xx redirects the client followed).
        resolved = _origin(str(resp.url)) or base_url.rstrip("/")
    finally:
        if owns_client:
            await client.aclose()
    # Build topic URLs against the resolved origin so links work post-redirect.
    items = _results_to_items(resolved, data.get("results", []))
    return ForumSearchResult(items=items, resolved_base_url=resolved)


async def fetch_topic(url: str, client: httpx.AsyncClient | None = None) -> list[TopicLink]:
    owns_client = client is None
    client = client or httpx.AsyncClient(timeout=OUTBOUND_TIMEOUT_SECONDS, follow_redirects=True)
    try:
        resp = await get_with_retry(client, url)
        resp.raise_for_status()
        html = resp.text
    finally:
        if owns_client:
            await client.aclose()
    # HTML parsing is CPU-bound; run it off the event loop so it doesn't block
    # other concurrent requests.
    return await asyncio.to_thread(parse_topic_html, html)
