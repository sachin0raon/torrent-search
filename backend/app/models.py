"""Pydantic response schemas for the API."""
from __future__ import annotations

from pydantic import BaseModel


# ---- Stage 1: TMDB search ----
class TitleResult(BaseModel):
    tmdb_id: int
    media_type: str  # "movie" | "tv"
    title: str
    year: str | None = None
    poster_url: str | None = None
    overview: str | None = None


class SearchResponse(BaseModel):
    results: list[TitleResult]


# ---- Stage 2: external ids ----
class ExternalIdsResponse(BaseModel):
    imdb_id: str | None = None


# ---- TV seasons (for season/episode dropdowns) ----
class TvSeason(BaseModel):
    season_number: int
    name: str | None = None
    episode_count: int


class TvSeasonsResponse(BaseModel):
    seasons: list[TvSeason]


# ---- Stage 3: streams (parallel fan-out) ----
class TorrentioItem(BaseModel):
    title: str
    seeders: int | None = None
    size: str | None = None
    source: str | None = None
    magnet: str


class ForumSearchItem(BaseModel):
    tid: int
    title: str
    topic_url: str


class SourceResult(BaseModel):
    ok: bool
    error: str | None = None
    items: list = []


class ForumSearchResponse(SourceResult):
    # Set when a forum-domain redirect was detected and the base URL was
    # auto-updated; the UI uses it to notify the user of the new URL.
    forum_base_updated: str | None = None


# ---- Stage 4: forum topic links ----
class TopicLink(BaseModel):
    filename: str | None = None
    file_url: str | None = None
    magnet: str | None = None


class TopicResponse(BaseModel):
    links: list[TopicLink]


# ---- Settings ----
class ConfigResponse(BaseModel):
    forum_base_url: str | None = None
    source: str  # "config" | "env"
    file_browser_url: str | None = None
    enable_streaming: bool = True


class ConfigUpdate(BaseModel):
    forum_base_url: str


# ---- 1337x Search & Magnet ----
class X1337SearchItem(BaseModel):
    title: str
    detail_path: str
    seeds: int = 0
    leeches: int = 0
    size: str | None = None
    category: str | None = None
    date: str | None = None


class X1337SearchResponse(SourceResult):
    items: list[X1337SearchItem] = []


class X1337MagnetResponse(BaseModel):
    magnet: str

