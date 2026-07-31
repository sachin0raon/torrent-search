import json
from pathlib import Path

from app.services.comet import build_stream_url, parse_stream_title, streams_to_items

COMET_BASE = "https://comet.feels.legal/eyJ0ZXN0Ijp0cnVlfQ==/stream"

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


# ---- Title parsing (Comet uses `description`, not `title`, and 🔎 not ⚙️) ----
def test_parse_full_description():
    raw = "📄 Movie.2026.2160p.mkv\n👤 42 💾 62.9 GB 🔎 Knaben"
    parsed = parse_stream_title(raw)
    assert parsed["title"] == "Movie.2026.2160p.mkv"
    assert parsed["seeders"] == 42
    assert parsed["size"] == "62.9 GB"
    assert parsed["source"] == "Knaben"


def test_parse_strips_document_emoji_even_without_metadata():
    parsed = parse_stream_title("📄 Just A Title")
    assert parsed["title"] == "Just A Title"
    assert parsed["seeders"] is None


def test_parse_seeders_with_comma():
    parsed = parse_stream_title("📄 Title\n👤 12,345 💾 2 GB 🔎 X")
    assert parsed["seeders"] == 12345


def test_parse_missing_metadata_debrid_cached():
    # Debrid-cached entries commonly carry no seeders/size/provider at all.
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
            "description": "📄 Movie A\n👤 42 💾 1.34 GB 🔎 Knaben",
            "infoHash": "633894b8378e4837dc551394c0637a35eb909c99",
            "behaviorHints": {"filename": "moviea.mkv"},
        },
        {
            "description": "📄 Movie B\n👤 5 💾 2.26 GB 🔎 StremThru",
            "infoHash": "4f3090b93b1520c36f7c13bc77dcb73bb5121685",
            "sources": [
                "tracker:http://bt4.t-ru.org/ann?magnet",
                "tracker:udp://tracker.publictracker.xyz:6969/announce",
            ],
        },
        {"description": "No hash here", "behaviorHints": {}},  # skipped
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
            "description": "📄 Movie\n👤 100 💾 1 GB 🔎 ProviderA",
            "infoHash": "633894b8378e4837dc551394c0637a35eb909c99",
        },
        {
            "description": "📄 Movie\n👤 50 💾 1 GB 🔎 ProviderB",
            "infoHash": "633894b8378e4837dc551394c0637a35eb909c99",
        },
        {
            "description": "📄 Movie\n👤 10 💾 1 GB 🔎 ProviderC",
            "infoHash": "4f3090b93b1520c36f7c13bc77dcb73bb5121685",
        },
    ]
    items = streams_to_items(streams)
    assert len(items) == 2
    assert len({i.magnet for i in items}) == 2


# ---- Comet-only seeders-descending sort, unknown (None) seeders last ----
def test_streams_to_items_sorts_by_seeders_descending_with_none_last():
    streams = [
        {
            "description": "📄 Low seeders\n👤 5 💾 1 GB 🔎 X",
            "infoHash": "1111111111111111111111111111111111111111",
        },
        {
            "description": "📄 No seeders (debrid-cached)",
            "infoHash": "2222222222222222222222222222222222222222",
        },
        {
            "description": "📄 High seeders\n👤 42 💾 1 GB 🔎 X",
            "infoHash": "3333333333333333333333333333333333333333",
        },
    ]
    items = streams_to_items(streams)
    assert [i.seeders for i in items] == [42, 5, None]


def test_streams_to_items_stable_order_among_none_seeders():
    streams = [
        {"description": "📄 First\n💾 1 GB", "infoHash": "aaaa000000000000000000000000000000000a"},
        {"description": "📄 Second\n💾 1 GB", "infoHash": "bbbb000000000000000000000000000000000b"},
    ]
    items = streams_to_items(streams)
    assert [i.title for i in items] == ["First", "Second"]


# ---- Real sample fixture (captured live during design) ----
def test_streams_to_items_against_fixture():
    data = json.loads((FIXTURES / "comet_streams.json").read_text())
    items = streams_to_items(data["streams"])
    assert len(items) == 3
    # Known-seeder items (42, 5) come before the unknown (None) debrid-cached one.
    assert [i.seeders for i in items] == [42, 5, None]
