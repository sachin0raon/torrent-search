import httpx
import pytest
import respx
from fastapi.testclient import TestClient


@pytest.fixture
def client(tmp_path, monkeypatch):
    # config values (paths, keys, base URL) are all read dynamically per call,
    # so setting env vars is enough — no module reloads required.
    monkeypatch.setenv("CONFIG_JSON_PATH", str(tmp_path / "config.json"))
    monkeypatch.setenv("TMDB_API_KEY", "test-key")
    monkeypatch.setenv("FORUM_BASE_URL", "https://forum.tld")
    # Zero backoff so retried failures (e.g. 500) don't add real sleeps to tests.
    monkeypatch.setenv("EXTERNAL_BACKOFF_BASE", "0")
    monkeypatch.setenv("EXTERNAL_BACKOFF_CAP", "0")
    monkeypatch.setenv("EXTERNAL_BACKOFF_JITTER", "0")

    from app.main import app

    # Context-manage so the lifespan runs and app.state.http (shared client) is set.
    with TestClient(app) as client:
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
def test_streams_partial_failure(client):
    # torrentio OK, forum fails -> partial result, forum ok=False.
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
    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(500)
    )

    resp = client.get(
        "/api/streams",
        params={"imdb_id": "tt1", "media_type": "movie", "raw_query": "movie"},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["torrentio"]["ok"] is True
    assert len(body["torrentio"]["items"]) == 1
    assert body["forum"]["ok"] is False
    assert body["forum"]["error"]


@respx.mock
def test_streams_skips_torrentio_without_imdb(client):
    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(200, json={"results": [{"tid": 5, "title": "A Title"}]})
    )
    resp = client.get(
        "/api/streams",
        params={"media_type": "movie", "raw_query": "movie"},
    )
    body = resp.json()
    assert body["torrentio"]["ok"] is False
    assert "IMDb" in body["torrentio"]["error"]
    assert body["forum"]["ok"] is True
    assert body["forum"]["items"][0]["topic_url"].endswith("/topic/5-a-title/")


@respx.mock
def test_streams_auto_updates_forum_base_on_redirect(client):
    # Old domain 301-redirects to a new domain that serves valid search JSON.
    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(
            301, headers={"Location": "https://new-forum.tld/search/api/search.php"}
        )
    )
    respx.get(url__regex=r"https://new-forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(200, json={"results": [{"tid": 7, "title": "New Title"}]})
    )

    resp = client.get("/api/streams", params={"media_type": "movie", "raw_query": "x"})
    body = resp.json()

    assert body["forum_base_updated"] == "https://new-forum.tld"
    assert body["forum"]["ok"] is True
    # Topic URLs are rebuilt against the new origin.
    assert body["forum"]["items"][0]["topic_url"].startswith("https://new-forum.tld/index.php")

    # The new base URL is persisted (config source becomes "config").
    cfg = client.get("/api/config").json()
    assert cfg["forum_base_url"] == "https://new-forum.tld"
    assert cfg["source"] == "config"


def test_config_roundtrip(client):
    # Starts from env default.
    r = client.get("/api/config").json()
    assert r == {"forum_base_url": "https://forum.tld", "source": "env"}

    # Override persists.
    r2 = client.put("/api/config", json={"forum_base_url": "https://new.tld/"}).json()
    assert r2 == {"forum_base_url": "https://new.tld", "source": "config"}

    r3 = client.get("/api/config").json()
    assert r3["source"] == "config"
    assert r3["forum_base_url"] == "https://new.tld"


def test_config_rejects_invalid(client):
    resp = client.put("/api/config", json={"forum_base_url": "not-a-url"})
    assert resp.status_code == 400
