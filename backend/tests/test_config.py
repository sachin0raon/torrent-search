import importlib

import pytest


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    """Reload the config module pointed at a temp config.json and clean env.

    Set (not delete) FORUM_BASE_URL to empty: config.py's module-level
    load_dotenv() re-runs on reload with override=False, so a *deleted* var
    would get silently refilled from a real backend/.env on disk — an empty
    string still counts as "present" to dotenv, so it's left alone, and
    get_forum_base_url() already treats an empty value as unset.
    """
    monkeypatch.setenv("CONFIG_JSON_PATH", str(tmp_path / "config.json"))
    monkeypatch.setenv("FORUM_BASE_URL", "")
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


def test_probe_settings_defaults(cfg, monkeypatch):
    for var in ("FORUM_PROBE_ENABLED", "FORUM_PROBE_INTERVAL_MINUTES", "FORUM_PROBE_QUERY"):
        monkeypatch.delenv(var, raising=False)
    s = cfg.get_forum_probe_settings()
    assert s.enabled is True
    assert s.interval_minutes == 30
    assert s.query == "a"


def test_probe_settings_from_env(cfg, monkeypatch):
    monkeypatch.setenv("FORUM_PROBE_ENABLED", "false")
    monkeypatch.setenv("FORUM_PROBE_INTERVAL_MINUTES", "120")
    monkeypatch.setenv("FORUM_PROBE_QUERY", "batman")
    s = cfg.get_forum_probe_settings()
    assert s.enabled is False
    assert s.interval_minutes == 120
    assert s.query == "batman"


def test_probe_interval_floored_at_one(cfg, monkeypatch):
    monkeypatch.setenv("FORUM_PROBE_INTERVAL_MINUTES", "0")
    assert cfg.get_forum_probe_settings().interval_minutes == 1


def test_enable_streaming_default(cfg, monkeypatch):
    monkeypatch.delenv("ENABLE_STREAMING", raising=False)
    assert cfg.get_enable_streaming() is True


def test_enable_streaming_override(cfg, monkeypatch):
    monkeypatch.setenv("ENABLE_STREAMING", "false")
    assert cfg.get_enable_streaming() is False
    monkeypatch.setenv("ENABLE_STREAMING", "0")
    assert cfg.get_enable_streaming() is False
