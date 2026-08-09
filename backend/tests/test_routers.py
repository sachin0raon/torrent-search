import httpx
import pytest
import respx
from fastapi.testclient import TestClient


@pytest.fixture(autouse=True)
def _clear_discover_cache():
    from app.services import discover_cache

    discover_cache._cache.clear()
    yield
    discover_cache._cache.clear()


@pytest.fixture
def client(tmp_path, monkeypatch):
    # config values (paths, keys, base URL) are all read dynamically per call,
    # so setting env vars is enough — no module reloads required.
    monkeypatch.setenv("CONFIG_JSON_PATH", str(tmp_path / "config.json"))
    monkeypatch.setenv("TMDB_API_KEY", "test-key")
    monkeypatch.setenv("FORUM_BASE_URL", "https://forum.tld")
    monkeypatch.setenv("FORUM_PROBE_ENABLED", "0")
    # Zero backoff so retried failures (e.g. 500) don't add real sleeps to tests.
    monkeypatch.setenv("EXTERNAL_BACKOFF_BASE", "0")
    monkeypatch.setenv("EXTERNAL_BACKOFF_CAP", "0")
    monkeypatch.setenv("EXTERNAL_BACKOFF_JITTER", "0")

    from app.main import app

    # Context-manage so the lifespan runs and app.state.http (shared client) is set.
    with TestClient(app, raise_server_exceptions=False) as client:
        yield client


def test_health(client):
    assert client.get("/api/health").json() == {"status": "ok"}


@respx.mock
def test_search_filters_person(client):
    respx.get("https://api.themoviedb.org/3/search/multi").mock(
        return_value=httpx.Response(
            200,
            json={
                "results": [
                    {"id": 1, "media_type": "movie", "title": "M", "release_date": "2020-01-01"},
                    {"id": 2, "media_type": "person", "name": "Somebody"},
                    {"id": 3, "media_type": "tv", "name": "Show", "first_air_date": "2019-05-05"},
                ]
            },
        )
    )
    resp = client.get("/api/search", params={"query": "x"})
    assert resp.status_code == 200
    results = resp.json()["results"]
    assert [r["tmdb_id"] for r in results] == [1, 3]
    assert results[0]["year"] == "2020"
    assert results[1]["media_type"] == "tv"


def test_config_roundtrip(client):
    # Starts from env default.
    r = client.get("/api/config").json()
    assert r["forum_base_url"] == "https://forum.tld"
    assert r["source"] == "env"

    # Override persists.
    r2 = client.put("/api/config", json={"forum_base_url": "https://new.tld/"}).json()
    assert r2["forum_base_url"] == "https://new.tld"
    assert r2["source"] == "config"

    r3 = client.get("/api/config").json()
    assert r3["source"] == "config"
    assert r3["forum_base_url"] == "https://new.tld"


@respx.mock
def test_search_dedups_repeated_title(client):
    # TMDB occasionally returns the same title twice in one response; a
    # duplicate (media_type, tmdb_id) would otherwise reach the frontend as
    # two list items sharing one React key.
    respx.get("https://api.themoviedb.org/3/search/multi").mock(
        return_value=httpx.Response(
            200,
            json={
                "results": [
                    {"id": 1, "media_type": "movie", "title": "M", "release_date": "2020-01-01"},
                    {"id": 1, "media_type": "movie", "title": "M", "release_date": "2020-01-01"},
                    {"id": 2, "media_type": "tv", "name": "Show", "first_air_date": "2019-05-05"},
                ]
            },
        )
    )
    resp = client.get("/api/search", params={"query": "x"})
    assert resp.status_code == 200
    results = resp.json()["results"]
    assert [(r["media_type"], r["tmdb_id"]) for r in results] == [("movie", 1), ("tv", 2)]


@respx.mock
def test_tv_seasons(client):
    respx.get("https://api.themoviedb.org/3/tv/99/external_ids")  # not used, just registered
    respx.get("https://api.themoviedb.org/3/tv/99").mock(
        return_value=httpx.Response(
            200,
            json={
                "seasons": [
                    {"season_number": 0, "name": "Specials", "episode_count": 3},
                    {"season_number": 1, "name": "Season 1", "episode_count": 10},
                    {"season_number": 2, "name": "Season 2", "episode_count": 8},
                    {"season_number": 3, "name": "Future", "episode_count": 0},  # dropped
                ]
            },
        )
    )
    resp = client.get("/api/tv-seasons", params={"tmdb_id": 99})
    assert resp.status_code == 200
    seasons = resp.json()["seasons"]
    # Season 3 (0 episodes) dropped; sorted by number.
    assert [s["season_number"] for s in seasons] == [0, 1, 2]
    assert seasons[1] == {"season_number": 1, "name": "Season 1", "episode_count": 10}


@respx.mock
def test_external_ids(client):
    respx.get("https://api.themoviedb.org/3/movie/42/external_ids").mock(
        return_value=httpx.Response(200, json={"imdb_id": "tt0042"})
    )
    resp = client.get("/api/external-ids", params={"media_type": "movie", "tmdb_id": 42})
    assert resp.json() == {"imdb_id": "tt0042"}


@respx.mock
def test_discover_trending_movie(client):
    respx.get("https://api.themoviedb.org/3/trending/movie/week").mock(
        return_value=httpx.Response(
            200,
            json={
                "results": [
                    {"id": 1, "title": "Movie A", "release_date": "2024-03-01", "poster_path": "/a.jpg"},
                    {"id": 2, "title": "Movie B", "release_date": "2023-01-01"},
                ]
            },
        )
    )
    resp = client.get("/api/discover", params={"category": "trending", "media_type": "movie"})
    assert resp.status_code == 200
    results = resp.json()["results"]
    assert [r["tmdb_id"] for r in results] == [1, 2]
    assert all(r["media_type"] == "movie" for r in results)
    assert results[0]["year"] == "2024"
    assert results[0]["poster_url"].endswith("/a.jpg")


@respx.mock
def test_discover_dedups_repeated_title(client):
    respx.get("https://api.themoviedb.org/3/trending/movie/week").mock(
        return_value=httpx.Response(
            200,
            json={
                "results": [
                    {"id": 1, "title": "Movie A", "release_date": "2024-03-01"},
                    {"id": 1, "title": "Movie A", "release_date": "2024-03-01"},
                    {"id": 2, "title": "Movie B", "release_date": "2023-01-01"},
                ]
            },
        )
    )
    resp = client.get("/api/discover", params={"category": "trending", "media_type": "movie"})
    assert resp.status_code == 200
    results = resp.json()["results"]
    assert [r["tmdb_id"] for r in results] == [1, 2]


@respx.mock
def test_discover_popular_tv(client):
    respx.get("https://api.themoviedb.org/3/tv/popular").mock(
        return_value=httpx.Response(
            200, json={"results": [{"id": 9, "name": "Show", "first_air_date": "2022-06-01"}]}
        )
    )
    resp = client.get("/api/discover", params={"category": "popular", "media_type": "tv"})
    assert resp.status_code == 200
    results = resp.json()["results"]
    assert results[0]["media_type"] == "tv"
    assert results[0]["title"] == "Show"


def test_discover_invalid_category(client):
    resp = client.get("/api/discover", params={"category": "bogus", "media_type": "movie"})
    assert resp.status_code == 422


@respx.mock
def test_discover_upstream_failure(client):
    respx.get("https://api.themoviedb.org/3/movie/top_rated").mock(
        return_value=httpx.Response(404)
    )
    resp = client.get("/api/discover", params={"category": "top_rated", "media_type": "movie"})
    assert resp.status_code == 502


@respx.mock
def test_discover_pagination_maps_two_ui_pages_to_one_tmdb_page(client):
    items = [
        {"id": i, "title": f"T{i}", "release_date": "2024-01-01"} for i in range(1, 21)
    ]
    route = respx.get("https://api.themoviedb.org/3/trending/movie/week").mock(
        return_value=httpx.Response(200, json={"results": items})
    )

    page1 = client.get(
        "/api/discover", params={"category": "trending", "media_type": "movie", "page": 1}
    ).json()["results"]
    page2 = client.get(
        "/api/discover", params={"category": "trending", "media_type": "movie", "page": 2}
    ).json()["results"]

    assert [r["tmdb_id"] for r in page1] == list(range(1, 11))
    assert [r["tmdb_id"] for r in page2] == list(range(11, 21))
    # Both UI pages come from the same cached TMDB page -> exactly one upstream call.
    assert route.call_count == 1


@respx.mock
def test_torrentio_endpoint_ok(client):
    respx.get(url__regex=r"https://torrentio\.strem\.fun/.*").mock(
        return_value=httpx.Response(
            200,
            json={
                "streams": [
                    {"title": "Movie\n👤 10 💾 1 GB ⚙️ BT", "infoHash": "HASH1"},
                ]
            },
        )
    )
    resp = client.get(
        "/api/torrentio",
        params={"imdb_id": "tt1", "media_type": "movie"},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["ok"] is True
    assert len(body["items"]) == 1


@respx.mock
def test_torrentio_endpoint_upstream_failure(client):
    respx.get(url__regex=r"https://torrentio\.strem\.fun/.*").mock(
        return_value=httpx.Response(500)
    )
    resp = client.get("/api/torrentio", params={"imdb_id": "tt1", "media_type": "movie"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["ok"] is False
    assert body["error"]


def test_torrentio_endpoint_skips_without_imdb(client):
    resp = client.get("/api/torrentio", params={"media_type": "movie"})
    body = resp.json()
    assert body["ok"] is False
    assert "IMDb" in body["error"]


@respx.mock
def test_comet_endpoint_ok(client):
    respx.get(url__regex=r"https://comet\.feels\.legal/.*").mock(
        return_value=httpx.Response(
            200,
            json={
                "streams": [
                    {"description": "📄 Movie\n👤 10 💾 1 GB 🔎 Knaben", "infoHash": "HASH1"},
                ]
            },
        )
    )
    resp = client.get("/api/comet", params={"imdb_id": "tt1", "media_type": "movie"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["ok"] is True
    assert len(body["items"]) == 1


def test_comet_endpoint_skips_without_imdb(client):
    resp = client.get("/api/comet", params={"media_type": "movie"})
    body = resp.json()
    assert body["ok"] is False
    assert "IMDb" in body["error"]


@respx.mock
def test_comet_endpoint_upstream_error(client):
    respx.get(url__regex=r"https://comet\.feels\.legal/.*").mock(
        return_value=httpx.Response(405)
    )
    resp = client.get("/api/comet", params={"imdb_id": "tt1", "media_type": "movie"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["ok"] is False
    assert "405" in body["error"]


@respx.mock
def test_meteor_endpoint_ok(client):
    respx.get(url__regex=r"https://meteorfortheweebs\.midnightignite\.me/.*").mock(
        return_value=httpx.Response(
            200,
            json={
                "streams": [
                    {"description": "📄 Movie\n📺 1080p\n💾 1 GB", "infoHash": "HASH1"},
                ]
            },
        )
    )
    resp = client.get("/api/meteor", params={"imdb_id": "tt1", "media_type": "movie"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["ok"] is True
    assert len(body["items"]) == 1


def test_meteor_endpoint_skips_without_imdb(client):
    resp = client.get("/api/meteor", params={"media_type": "movie"})
    body = resp.json()
    assert body["ok"] is False
    assert "IMDb" in body["error"]


@respx.mock
def test_meteor_endpoint_upstream_error(client):
    respx.get(url__regex=r"https://meteorfortheweebs\.midnightignite\.me/.*").mock(
        return_value=httpx.Response(502)
    )
    resp = client.get("/api/meteor", params={"imdb_id": "tt1", "media_type": "movie"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["ok"] is False
    assert "502" in body["error"]


@respx.mock
def test_forum_search_ok(client):
    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(200, json={"results": [{"tid": 5, "title": "A Title"}]})
    )
    resp = client.get("/api/forum/search", params={"raw_query": "movie"})
    body = resp.json()
    assert body["ok"] is True
    assert body["items"][0]["topic_url"].endswith("/topic/5-a-title/")


@respx.mock
def test_forum_search_upstream_failure(client):
    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(500)
    )
    resp = client.get("/api/forum/search", params={"raw_query": "movie"})
    body = resp.json()
    assert body["ok"] is False
    assert body["error"]


@respx.mock
def test_forum_search_auto_updates_forum_base_on_redirect(client):
    # Old domain 301-redirects to a new domain that serves valid search JSON.
    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(
            301, headers={"Location": "https://new-forum.tld/search/api/search.php"}
        )
    )
    respx.get(url__regex=r"https://new-forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(200, json={"results": [{"tid": 7, "title": "New Title"}]})
    )

    resp = client.get("/api/forum/search", params={"raw_query": "x"})
    body = resp.json()

    assert body["forum_base_updated"] == "https://new-forum.tld"
    assert body["ok"] is True
    # Topic URLs are rebuilt against the new origin.
    assert body["items"][0]["topic_url"].startswith("https://new-forum.tld/index.php")

    # The new base URL is persisted (config source becomes "config").
    cfg = client.get("/api/config").json()
    assert cfg["forum_base_url"] == "https://new-forum.tld"
    assert cfg["source"] == "config"


def test_config_roundtrip(client):
    # Starts from env default.
    r = client.get("/api/config").json()
    assert r["forum_base_url"] == "https://forum.tld"
    assert r["source"] == "env"

    # Override persists.
    r2 = client.put("/api/config", json={"forum_base_url": "https://new.tld/"}).json()
    assert r2["forum_base_url"] == "https://new.tld"
    assert r2["source"] == "config"

    r3 = client.get("/api/config").json()
    assert r3["source"] == "config"
    assert r3["forum_base_url"] == "https://new.tld"


def test_config_rejects_invalid(client):
    resp = client.put("/api/config", json={"forum_base_url": "not-a-url"})
    assert resp.status_code == 400, f"Expected 400 but got {resp.status_code}: {resp.text}"
