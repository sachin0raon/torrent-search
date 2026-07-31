import json
from pathlib import Path

from app.services.meteor import build_stream_url, parse_stream_title, streams_to_items

FIXTURES = Path(__file__).parent / "fixtures"


# ---- URL building ----
def test_movie_url():
    assert build_stream_url("tt123", "movie").endswith("/movie/tt123.json")


def test_series_whole_url():
    assert build_stream_url("tt123", "tv").endswith("/series/tt123.json")


def test_series_episode_url():
    assert build_stream_url("tt123", "tv", season=2, episode=5).endswith(
        "/series/tt123:2:5.json"
    )


def test_series_partial_falls_back_to_whole():
    assert build_stream_url("tt123", "tv", season=2).endswith("/series/tt123.json")


# ---- Title parsing (Meteor uses `description`; 📺 quality goes in `source`) ----
def test_parse_full_description():
    raw = "📄 Movie.2026.1080p.WEB.h264-ETHEL\n📺 1080p | webdl | h264\n💾 5.45 GiB"
    parsed = parse_stream_title(raw)
    assert parsed["title"] == "Movie.2026.1080p.WEB.h264-ETHEL"
    assert parsed["size"] == "5.45 GiB"
    assert parsed["source"] == "1080p | webdl | h264"
    # Most Meteor entries omit seeders entirely.
    assert parsed["seeders"] is None


def test_parse_seeders_when_present():
    # Opportunistic match: some titles/configs may still surface a seeder count.
    parsed = parse_stream_title("📄 Title\n👤 12,345 💾 2 GB 📺 720p")
    assert parsed["seeders"] == 12345


def test_parse_missing_metadata():
    parsed = parse_stream_title("📄 Just A Title")
    assert parsed["title"] == "Just A Title"
    assert parsed["seeders"] is None
    assert parsed["size"] is None
    assert parsed["source"] is None


def test_parse_empty_description():
    parsed = parse_stream_title("")
    assert parsed["title"] == ""
    assert parsed["seeders"] is None


# ---- streams_to_items ----
def test_streams_to_items_builds_magnets_and_skips_missing_hash():
    streams = [
        {
            "description": "📄 Movie A\n📺 720p | webrip\n💾 969 MiB",
            "infoHash": "bac40d5cd3a67bb3659b14ad694776f8f0341d09",
            "sources": [
                "dht:bac40d5cd3a67bb3659b14ad694776f8f0341d09",
                "tracker:udp://tracker.opentrackr.org:1337/announce",
            ],
        },
        {
            "description": "📄 Movie B\n📺 1080p | webdl | h264\n💾 5.45 GiB",
            "infoHash": "710d6257964361671a03fd4ab968a4dddae9a040",
            "sources": [
                "tracker:udp://tracker.opentrackr.org:1337/announce",
                "tracker:udp://open.tracker.cl:1337/announce",
            ],
        },
        {"description": "No hash here"},  # skipped
    ]
    items = streams_to_items(streams)
    assert len(items) == 2
    assert items[0].magnet.startswith("magnet:?dn=")
    assert items[1].magnet.count("&tr=") == 2


def test_streams_to_items_empty():
    assert streams_to_items([]) == []


def test_streams_to_items_dedups_identical_magnet():
    streams = [
        {
            "description": "📄 Movie\n📺 720p\n💾 1 GB",
            "infoHash": "633894b8378e4837dc551394c0637a35eb909c99",
        },
        {
            "description": "📄 Movie\n📺 720p\n💾 1 GB",
            "infoHash": "633894b8378e4837dc551394c0637a35eb909c99",
        },
        {
            "description": "📄 Movie\n📺 1080p\n💾 2 GB",
            "infoHash": "4f3090b93b1520c36f7c13bc77dcb73bb5121685",
        },
    ]
    items = streams_to_items(streams)
    assert len(items) == 2
    assert len({i.magnet for i in items}) == 2


# ---- No local sort for Meteor: upstream config already requests seeders-first ----
def test_streams_to_items_preserves_upstream_order():
    streams = [
        {"description": "📄 First\n📺 720p\n💾 1 GB", "infoHash": "aaaa000000000000000000000000000000000a"},
        {"description": "📄 Second\n📺 1080p\n💾 2 GB", "infoHash": "bbbb000000000000000000000000000000000b"},
    ]
    items = streams_to_items(streams)
    assert [i.title for i in items] == ["First", "Second"]


# ---- Real sample fixture (captured live during design) ----
def test_streams_to_items_against_fixture():
    data = json.loads((FIXTURES / "meteor_streams.json").read_text())
    items = streams_to_items(data["streams"])
    assert len(items) == 2
    assert items[0].source == "720p | webrip"
    assert items[1].size == "5.45 GiB"
