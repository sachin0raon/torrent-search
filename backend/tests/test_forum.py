from pathlib import Path

from app.services.forum import (
    build_slug,
    build_topic_url,
    parse_topic_html,
)

FIXTURES = Path(__file__).parent / "fixtures"


# ---- slug ----
def test_slug_basic():
    assert build_slug("Linux mint cinnamon desktop amd64 iso") == "linux-mint-cinnamon-desktop-amd64-iso"


def test_slug_strips_special_chars_keeps_hyphen():
    assert build_slug("Movie: The Sequel (2024) [1080p]") == "movie-the-sequel-2024-1080p"


def test_slug_preexisting_hyphens_kept():
    assert build_slug("well-known title") == "well-known-title"


def test_slug_collapses_repeats():
    assert build_slug("a  --  b") == "a-b"


def test_slug_truncates_to_80():
    title = "word " * 40  # very long
    slug = build_slug(title)
    assert len(slug) <= 80


def test_slug_trailing_hyphen_allowed():
    # Truncation lands right after a hyphen boundary -> trailing hyphen preserved.
    title = "a" * 79 + " b"
    slug = build_slug(title)
    assert len(slug) == 80
    assert slug.endswith("-")


def test_slug_unicode_dropped():
    assert build_slug("Amélie café") == "amlie-caf"


def test_build_topic_url():
    url = build_topic_url("https://forum.tld", 4630, "Linux mint cinnamon desktop amd64 iso")
    assert url == "https://forum.tld/index.php?/topic/4630-linux-mint-cinnamon-desktop-amd64-iso/"


def test_build_topic_url_strips_trailing_slash_on_base():
    url = build_topic_url("https://forum.tld/", 1, "x")
    assert url == "https://forum.tld/index.php?/topic/1-x/"


# ---- topic HTML parsing ----
def test_parse_topic_pairs_and_edges():
    html = (FIXTURES / "topic.html").read_text(encoding="utf-8")
    links = parse_topic_html(html)

    # pair + unpaired file + pair + stray magnet = 4 rows.
    assert len(links) == 4

    # Paired: file immediately followed by its magnet.
    assert links[0].filename == "Movie.One.1080p.torrent"
    assert links[0].file_url.endswith("id=1")
    assert links[0].magnet.startswith("magnet:?xt=urn:btih:HASH1")

    # Unpaired file (next anchor was another file) -> magnet None.
    assert links[1].filename == "Movie.Two.720p.torrent"
    assert links[1].magnet is None

    # Paired again.
    assert links[2].filename == "Movie.Three.480p.torrent"
    assert links[2].magnet.startswith("magnet:?xt=urn:btih:HASH3")

    # Stray magnet -> named from its `dn` parameter (dn=Stray in the fixture).
    assert links[3].filename == "Stray"
    assert links[3].magnet.startswith("magnet:?xt=urn:btih:HASH_STRAY")


def test_parse_topic_magnet_before_block_file():
    # "block" attachments often have the magnet BEFORE the file link.
    html = """
    <a href="magnet:?xt=urn:btih:HASHB">Magnet</a>
    <a class="ipsAttachLink ipsAttachLink_block" href="/f?id=9">Cool.Release.torrent</a>
    """
    links = parse_topic_html(html)
    assert len(links) == 1
    assert links[0].filename == "Cool.Release.torrent"
    assert links[0].file_url == "/f?id=9"
    assert links[0].magnet.startswith("magnet:?xt=urn:btih:HASHB")


def test_parse_topic_block_only_class():
    html = '<a class="ipsAttachLink_block" href="/f?id=1">A.torrent</a><a href="magnet:?xt=1">m</a>'
    links = parse_topic_html(html)
    assert len(links) == 1
    assert links[0].filename == "A.torrent"
    assert links[0].magnet == "magnet:?xt=1"


def test_parse_topic_real_ips_block():
    # Real IPS layout: torrent link (data-fileext="torrent") with filename in a
    # <span>, wrapped in <strong>, then a <br>, then a magnet button.
    html = (
        '<strong><a data-fileext="torrent" data-fileid="138165" '
        'href="https://site/attachment.php?id=138165&amp;key=abc" class="ipsAttachLink_block">'
        '<span>Operation.Endgame.2010.1080p.mkv.torrent</span></a></strong><br>'
        '<a class="skyblue-button" href="magnet:?xt=urn:btih:01408e80&amp;dn=x">MAGNET</a>'
    )
    links = parse_topic_html(html)
    assert len(links) == 1
    assert links[0].filename == "Operation.Endgame.2010.1080p.mkv.torrent"
    assert "attachment.php?id=138165" in links[0].file_url
    assert links[0].magnet.startswith("magnet:?xt=urn:btih:01408e80")


def test_parse_topic_excludes_image_attachments():
    # An image attachment (screenshot) carries the base ipsAttachLink class but
    # must not be treated as a torrent file nor steal the magnet.
    html = (
        '<a class="ipsAttachLink ipsAttachLink_image" data-fileext="jpg" href="/pic.jpg">shot.jpg</a>'
        '<a class="ipsAttachLink_block" data-fileext="torrent" href="/f?id=5">Movie.torrent</a>'
        '<a class="skyblue-button" href="magnet:?xt=urn:btih:H5">MAGNET</a>'
    )
    links = parse_topic_html(html)
    assert len(links) == 1
    assert links[0].filename == "Movie.torrent"
    assert links[0].magnet.endswith("H5")


def test_parse_topic_image_before_magnet_does_not_steal():
    # magnet, then image, then torrent -> torrent (via prev) must get the magnet.
    html = (
        '<a class="skyblue-button" href="magnet:?xt=urn:btih:H6">MAGNET</a>'
        '<a class="ipsAttachLink ipsAttachLink_image" data-fileext="png" href="/p.png">shot.png</a>'
        '<a class="ipsAttachLink_block" data-fileext="torrent" href="/f?id=6">Show.torrent</a>'
    )
    links = parse_topic_html(html)
    assert len(links) == 1
    assert links[0].filename == "Show.torrent"
    assert links[0].magnet.endswith("H6")


def test_parse_topic_magnet_only_named_from_dn():
    # Real case: server HTML has only magnet links (attachment links are JS-loaded).
    # Each magnet should be named from its url-encoded `dn` parameter.
    html = (
        '<a href="magnet:?xt=urn:btih:55c0&amp;dn=Murder%20Mystery%20%282019%29%201080p">m1</a>'
        '<a href="magnet:?xt=urn:btih:5c5c&amp;dn=Another.Movie.2020.720p">m2</a>'
    )
    links = parse_topic_html(html)
    assert len(links) == 2
    assert links[0].filename == "Murder Mystery (2019) 1080p"
    assert links[0].file_url is None
    assert links[0].magnet.startswith("magnet:?xt=urn:btih:55c0")
    assert links[1].filename == "Another.Movie.2020.720p"


def test_parse_topic_magnet_without_dn_stays_unnamed():
    links = parse_topic_html('<a href="magnet:?xt=urn:btih:abcd">m</a>')
    assert len(links) == 1
    assert links[0].filename is None
    assert links[0].magnet.startswith("magnet:?xt=urn:btih:abcd")


def test_parse_topic_no_links():
    links = parse_topic_html("<html><body><p>nothing here</p></body></html>")
    assert links == []
