from app.services.torrentio import (
    build_stream_url,
    parse_stream_title,
    streams_to_items,
)

TORRENTIO_BASE = "https://torrentio.strem.fun/sort=seeders|qualityfilter=480p/stream"


# ---- URL building ----
def test_movie_url():
    assert build_stream_url("tt123", "movie") == f"{TORRENTIO_BASE}/movie/tt123.json"


def test_series_whole_url():
    assert build_stream_url("tt123", "tv") == f"{TORRENTIO_BASE}/series/tt123.json"


def test_series_episode_url():
    assert (
        build_stream_url("tt123", "tv", season=2, episode=5)
        == f"{TORRENTIO_BASE}/series/tt123:2:5.json"
    )


def test_series_partial_falls_back_to_whole():
    # Only season, no episode -> whole series.
    assert build_stream_url("tt123", "tv", season=2) == f"{TORRENTIO_BASE}/series/tt123.json"


# ---- Title parsing ----
def test_parse_full_title():
    raw = "Ubuntu 22.04 desktop amd64\n👤 3945 💾 1.34 GB ⚙️ Bittorrent"
    parsed = parse_stream_title(raw)
    assert parsed["title"] == "Ubuntu 22.04 desktop amd64"
    assert parsed["seeders"] == 3945
    assert parsed["size"] == "1.34 GB"
    assert parsed["source"] == "Bittorrent"


def test_parse_seeders_with_comma():
    parsed = parse_stream_title("Title\n👤 12,345 💾 2 GB ⚙️ X")
    assert parsed["seeders"] == 12345


def test_parse_missing_metadata():
    parsed = parse_stream_title("Just A Title")
    assert parsed["title"] == "Just A Title"
    assert parsed["seeders"] is None
    assert parsed["size"] is None
    assert parsed["source"] is None


def test_parse_empty_title():
    parsed = parse_stream_title("")
    assert parsed["title"] == ""
    assert parsed["seeders"] is None


# ---- streams_to_items ----
def test_streams_to_items_builds_magnets_and_skips_missing_hash():
    streams = [
        {
            "title": "Ubuntu 22.04\n👤 3945 💾 1.34 GB ⚙️ Bittorrent",
            "infoHash": "633894b8378e4837dc551394c0637a35eb909c99",
            "behaviorHints": {"filename": "ubuntu.iso"},
        },
        {
            "title": "Ubuntu 26\n👤 1813 💾 2.26 GB ⚙️ Bittorrent",
            "infoHash": "4f3090b93b1520c36f7c13bc77dcb73bb5121685",
            "sources": [
                "tracker:http://bt4.t-ru.org/ann?magnet",
                "tracker:udp://tracker.publictracker.xyz:6969/announce",
            ],
        },
        {"title": "No hash here", "behaviorHints": {}},  # skipped
    ]
    items = streams_to_items(streams)
    assert len(items) == 2
    assert items[0].seeders == 3945
    assert items[0].magnet.startswith("magnet:?dn=")
    assert items[1].magnet.count("&tr=") == 2


def test_streams_to_items_empty():
    assert streams_to_items([]) == []
