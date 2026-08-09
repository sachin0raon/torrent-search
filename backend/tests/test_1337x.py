import pytest
from fastapi.testclient import TestClient

from app.services.x1337 import (
    build_d1_url,
    hash_path,
    parse_magnet_html,
    parse_search_html,
)


def test_hash_path():
    path = "/search/ubuntu/1/"
    h = hash_path(path)
    assert len(h) == 64  # SHA-256 hex string
    # Same input produces identical hash
    assert hash_path(path) == h


def test_build_d1_url():
    path = "/search/no way home/1/"
    d1_url = build_d1_url(path)
    assert d1_url.startswith("https://1337x-d1-static-api.zindex.eu.org/d1-web-api/")
    assert "?search_path=" in d1_url


def test_parse_search_html_valid():
    sample_html = """
    <table>
        <tr>
            <td class="name"><a href="/torrent/5178882/Spider-Man-No-Way-Home-2021-1080p/">Spider-Man No Way Home</a></td>
            <td class="seeds">7221</td>
            <td class="leeches">1327</td>
            <td class="size">2.8 GB7221</td>
            <td class="coll-date">Mar. 15th '22</td>
        </tr>
        <tr>
            <td class="name"><a href="/torrent/123456/Batman-2022/">The Batman 2022</a></td>
            <td class="seeds">500</td>
            <td class="leeches">50</td>
            <td class="size">1.5 GB</td>
            <td class="coll-date">Apr. 1st '22</td>
        </tr>
    </table>
    """
    items = parse_search_html(sample_html)
    assert len(items) == 2

    assert items[0].title == "Spider-Man No Way Home"
    assert items[0].detail_path == "/torrent/5178882/Spider-Man-No-Way-Home-2021-1080p/"
    assert items[0].seeds == 7221
    assert items[0].leeches == 1327
    assert items[0].size == "2.8 GB"

    assert items[1].title == "The Batman 2022"
    assert items[1].seeds == 500
    assert items[1].leeches == 50
    assert items[1].size == "1.5 GB"


def test_parse_search_html_empty():
    assert parse_search_html("") == []
    assert parse_search_html("<div>No results</div>") == []


def test_parse_magnet_html_valid():
    sample_html = """
    <html>
        <body>
            <a href="magnet:?xt=urn:btih:1234567890abcdef&dn=Spider-Man">Download Magnet</a>
        </body>
    </html>
    """
    magnet = parse_magnet_html(sample_html)
    assert magnet == "magnet:?xt=urn:btih:1234567890abcdef&dn=Spider-Man"


def test_parse_magnet_html_missing():
    sample_html = "<html><body><p>No magnet link here</p></body></html>"
    assert parse_magnet_html(sample_html) is None


@pytest.fixture
def client(tmp_path, monkeypatch):
    monkeypatch.setenv("CONFIG_JSON_PATH", str(tmp_path / "config.json"))
    monkeypatch.setenv("TMDB_API_KEY", "test-key")
    monkeypatch.setenv("FORUM_BASE_URL", "https://forum.tld")
    monkeypatch.setenv("FORUM_PROBE_ENABLED", "0")
    from app.main import app

    with TestClient(app, raise_server_exceptions=False) as c:
        yield c



def test_api_1337x_search_endpoint(client, monkeypatch):
    from app.models import X1337SearchItem

    async def mock_search(query, client=None):
        return [
            X1337SearchItem(
                title="Mock Torrent",
                detail_path="/torrent/100/Mock-Torrent/",
                seeds=10,
                leeches=2,
                size="1.2 GB",
            )
        ]

    monkeypatch.setattr("app.services.x1337.search_1337x", mock_search)

    response = client.get("/api/1337x/search?q=test")
    assert response.status_code == 200
    data = response.json()
    assert data["ok"] is True
    assert len(data["items"]) == 1
    assert data["items"][0]["title"] == "Mock Torrent"


def test_api_1337x_magnet_endpoint(client, monkeypatch):
    async def mock_fetch_magnet(path, client=None):
        return "magnet:?xt=urn:btih:mockhash"

    monkeypatch.setattr("app.services.x1337.fetch_1337x_magnet", mock_fetch_magnet)

    response = client.get("/api/1337x/magnet?path=/torrent/100/Mock-Torrent/")
    assert response.status_code == 200
    data = response.json()
    assert data["magnet"] == "magnet:?xt=urn:btih:mockhash"
