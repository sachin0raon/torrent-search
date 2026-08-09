"""1337x scraper service: search 1337x static D1 API and extract magnet links."""
from __future__ import annotations

import base64
import hashlib
import logging
import re
from urllib.parse import quote

import httpx
from bs4 import BeautifulSoup

from app.config import OUTBOUND_TIMEOUT_SECONDS, REQUEST_HEADERS
from app.models import X1337SearchItem
from app.services.http import get_with_retry

log = logging.getLogger("app.x1337")

D1_API_ENDPOINT = "https://1337x-d1-static-api.zindex.eu.org/d1-web-api"
BASE_DOMAIN = "https://1337x.to"


def hash_path(path: str) -> str:
    """Generate SHA-256 hash of the full 1337x URL path."""
    full_url = BASE_DOMAIN + path
    return hashlib.sha256(full_url.encode("utf-8")).hexdigest()


def build_d1_url(path: str) -> str:
    """Build the D1 API endpoint URL for a given 1337x path."""
    h = hash_path(path)
    b64_path = base64.b64encode(path.encode("utf-8")).decode("utf-8")
    return f"{D1_API_ENDPOINT}/{h}?search_path={quote(b64_path)}"


def parse_search_html(html: str) -> list[X1337SearchItem]:
    """Parse 1337x search results HTML table into X1337SearchItem objects."""
    if not html:
        return []
    soup = BeautifulSoup(html, "html.parser")
    rows = soup.select("tr")
    items: list[X1337SearchItem] = []

    for row in rows:
        name_a = row.select_one('td.name a[href*="/torrent/"]')
        if not name_a:
            continue

        detail_path = name_a.get("href", "")
        title = name_a.text.strip()
        if not detail_path or not title:
            continue

        seeds_el = row.select_one("td.seeds")
        seeds = 0
        if seeds_el:
            seeds_text = re.sub(r"[^\d]", "", seeds_el.text)
            if seeds_text:
                seeds = int(seeds_text)

        leeches_el = row.select_one("td.leeches")
        leeches = 0
        if leeches_el:
            leeches_text = re.sub(r"[^\d]", "", leeches_el.text)
            if leeches_text:
                leeches = int(leeches_text)

        size_el = row.select_one("td.size")
        size = None
        if size_el:
            # size text often includes seeder count concatenated at end or clean text
            raw_size = size_el.text.strip()
            # Extract standard size pattern like "2.8 GB" or "500 MB"
            m = re.search(r"(\d+(?:\.\d+)?\s*[KMGT]B)", raw_size, re.IGNORECASE)
            if m:
                size = m.group(1)
            else:
                size = raw_size or None

        date_el = row.select_one("td.coll-date")
        date = date_el.text.strip() if date_el else None

        items.append(
            X1337SearchItem(
                title=title,
                detail_path=detail_path,
                seeds=seeds,
                leeches=leeches,
                size=size,
                date=date,
            )
        )

    return items


def parse_magnet_html(html: str) -> str | None:
    """Extract magnet link from 1337x torrent detail page HTML."""
    if not html:
        return None
    soup = BeautifulSoup(html, "html.parser")
    magnet_a = soup.select_one('a[href^="magnet:"]')
    if magnet_a and magnet_a.get("href"):
        return magnet_a["href"]
    return None


async def search_1337x(
    query: str, client: httpx.AsyncClient | None = None
) -> list[X1337SearchItem]:
    """Search 1337x via D1 static API."""
    clean_query = query.strip()
    if not clean_query:
        return []

    path = f"/search/{quote(clean_query)}/1/"
    d1_url = build_d1_url(path)

    owns_client = client is None
    client = client or httpx.AsyncClient(
        timeout=OUTBOUND_TIMEOUT_SECONDS, follow_redirects=True, headers=REQUEST_HEADERS
    )
    try:
        resp = await get_with_retry(client, d1_url)
        resp.raise_for_status()
        return parse_search_html(resp.text)
    except Exception as e:
        log.warning("Failed to fetch 1337x search results for query %r: %s", query, e)
        raise
    finally:
        if owns_client:
            await client.aclose()


async def fetch_1337x_magnet(
    torrent_path: str, client: httpx.AsyncClient | None = None
) -> str | None:
    """Fetch detail page and extract magnet URI."""
    clean_path = torrent_path.strip()
    if not clean_path.startswith("/"):
        clean_path = "/" + clean_path

    d1_url = build_d1_url(clean_path)

    owns_client = client is None
    client = client or httpx.AsyncClient(
        timeout=OUTBOUND_TIMEOUT_SECONDS, follow_redirects=True, headers=REQUEST_HEADERS
    )
    try:
        resp = await get_with_retry(client, d1_url)
        resp.raise_for_status()
        return parse_magnet_html(resp.text)
    except Exception as e:
        log.warning("Failed to fetch 1337x detail page for path %r: %s", torrent_path, e)
        raise
    finally:
        if owns_client:
            await client.aclose()
