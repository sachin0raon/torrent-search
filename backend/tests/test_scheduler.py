import importlib

import httpx
import pytest
import respx


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    """Reload config pointed at a temp config.json with a known base URL."""
    monkeypatch.setenv("CONFIG_JSON_PATH", str(tmp_path / "config.json"))
    monkeypatch.setenv("FORUM_BASE_URL", "https://forum.tld")
    import app.config as config

    importlib.reload(config)
    # forum imports config symbols at module load; reload so it binds the reloaded config.
    import app.services.forum as forum

    importlib.reload(forum)
    return config


@respx.mock
async def test_probe_persists_redirected_base(cfg):
    from app.services import scheduler

    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(
            301, headers={"Location": "https://new-forum.tld/search/api/search.php"}
        )
    )
    respx.get(url__regex=r"https://new-forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(200, json={"results": []})
    )

    async with httpx.AsyncClient(follow_redirects=True) as client:
        await scheduler.probe_forum_base(client, query="a")

    url, source = cfg.get_forum_base_url()
    assert url == "https://new-forum.tld"
    assert source == "config"


@respx.mock
async def test_probe_noop_when_no_redirect(cfg):
    from app.services import scheduler

    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        return_value=httpx.Response(200, json={"results": []})
    )

    async with httpx.AsyncClient(follow_redirects=True) as client:
        await scheduler.probe_forum_base(client, query="a")

    # No override written: base still comes from env.
    _, source = cfg.get_forum_base_url()
    assert source == "env"


@respx.mock
async def test_probe_swallows_failure(cfg):
    from app.services import scheduler

    respx.get(url__regex=r"https://forum\.tld/search/api/search\.php.*").mock(
        side_effect=httpx.ConnectError("boom")
    )

    async with httpx.AsyncClient(follow_redirects=True) as client:
        # Must not raise — a failed probe just logs and returns.
        await scheduler.probe_forum_base(client, query="a")

    _, source = cfg.get_forum_base_url()
    assert source == "env"
