from urllib.parse import quote_plus

from app.utils.magnet import build_magnet, tracker_sources


def test_multiple_tracker_sources_retained_and_encoded():
    sources = [
        "tracker:http://bt4.t-ru.org/ann?magnet",
        "tracker:udp://tracker.publictracker.xyz:6969/announce",
    ]
    magnet = build_magnet("Ubuntu 26 desktop", "ABC123", sources)
    assert magnet.startswith("magnet:?dn=")
    assert "&xt=urn:btih:ABC123" in magnet
    # Full "tracker:" prefix retained, value url-encoded.
    for src in sources:
        assert f"&tr={quote_plus(src)}" in magnet
    assert magnet.count("&tr=") == 2


def test_non_tracker_sources_filtered_out():
    sources = [
        "tracker:udp://tracker.example:6969/announce",
        "dht:something",
        "http://not-a-tracker/announce",
    ]
    assert tracker_sources(sources) == ["tracker:udp://tracker.example:6969/announce"]
    magnet = build_magnet("Title", "HASH", sources)
    assert magnet.count("&tr=") == 1


def test_no_sources_has_only_dn_and_xt():
    magnet = build_magnet("My Movie", "DEADBEEF", None)
    assert magnet == "magnet:?dn=My+Movie&xt=urn:btih:DEADBEEF"
    assert "&tr=" not in magnet


def test_empty_sources_list():
    magnet = build_magnet("My Movie", "HASH", [])
    assert "&tr=" not in magnet


def test_special_chars_in_title_encoded():
    magnet = build_magnet("A & B: C/D (2024)", "HASH")
    assert "dn=" + quote_plus("A & B: C/D (2024)") in magnet
    # Raw special chars must not leak into the dn value.
    assert "A & B" not in magnet
