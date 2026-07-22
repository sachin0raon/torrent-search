import importlib

import pytest


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    """Reload the config module pointed at a temp config.json and clean env."""
    monkeypatch.setenv("CONFIG_JSON_PATH", str(tmp_path / "config.json"))
    monkeypatch.delenv("FORUM_BASE_URL", raising=False)
    import app.config as config

    importlib.reload(config)
    return config


def test_normalize_base_url_strips_trailing_slash(cfg):
    assert cfg.normalize_base_url("https://forum.tld/") == "https://forum.tld"


def test_normalize_base_url_rejects_invalid(cfg):
    for bad in ["", "not-a-url", "ftp://forum.tld", "forum.tld"]:
        with pytest.raises(cfg.ConfigError):
            cfg.normalize_base_url(bad)


def test_env_default_used_when_no_config(cfg, monkeypatch):
    monkeypatch.setenv("FORUM_BASE_URL", "https://env-forum.tld/")
    url, source = cfg.get_forum_base_url()
    assert url == "https://env-forum.tld"
    assert source == "env"


def test_config_json_overrides_env(cfg, monkeypatch):
    monkeypatch.setenv("FORUM_BASE_URL", "https://env-forum.tld")
    saved = cfg.set_forum_base_url("https://override.tld/")
    assert saved == "https://override.tld"
    url, source = cfg.get_forum_base_url()
    assert url == "https://override.tld"
    assert source == "config"


def test_no_config_no_env_returns_none(cfg):
    url, source = cfg.get_forum_base_url()
    assert url is None
    assert source == "env"


def test_set_invalid_url_raises_and_does_not_write(cfg):
    with pytest.raises(cfg.ConfigError):
        cfg.set_forum_base_url("garbage")
    assert not cfg.config_path().exists()
