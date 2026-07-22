"""Magnet-link construction for torrentio streams.

Format (per design):
    magnet:?dn={enc(display_title)}&xt=urn:btih:{infoHash}
        + one &tr={enc(source)} per sources[] entry that starts with "tracker:"

Notes:
- The full "tracker:" prefix is RETAINED inside the encoded &tr= value.
- Sources not starting with "tracker:" are filtered out.
- display_title is the first line of the newline-separated torrentio title.
"""
from __future__ import annotations

from urllib.parse import quote_plus

TRACKER_PREFIX = "tracker:"


def tracker_sources(sources: list[str] | None) -> list[str]:
    """Return only the sources that start with 'tracker:' (prefix retained)."""
    if not sources:
        return []
    return [s for s in sources if isinstance(s, str) and s.startswith(TRACKER_PREFIX)]


def build_magnet(display_title: str, info_hash: str, sources: list[str] | None = None) -> str:
    """Build a magnet URI from a display title, infoHash, and optional sources."""
    parts = [f"magnet:?dn={quote_plus(display_title)}", f"xt=urn:btih:{info_hash}"]
    for src in tracker_sources(sources):
        parts.append(f"tr={quote_plus(src)}")
    # First separator is '?', the rest are '&'. We built with '?' on the first part.
    return parts[0] + "".join(f"&{p}" for p in parts[1:])
