import asyncio

import pytest

from app.services import discover_cache as dc


@pytest.fixture(autouse=True)
def _clear_cache():
    dc._cache.clear()
    yield
    dc._cache.clear()


@pytest.fixture
def fake_clock(monkeypatch):
    state = {"now": 0.0}
    monkeypatch.setattr(dc.time, "monotonic", lambda: state["now"])
    return state


async def test_cache_hit_within_ttl(monkeypatch, fake_clock):
    monkeypatch.setenv("DISCOVER_CACHE_TTL_SECONDS", "100")
    calls = 0

    async def fetch():
        nonlocal calls
        calls += 1
        return ["item"]

    first = await dc.get_or_fetch("trending", "movie", 1, fetch)
    fake_clock["now"] = 50
    second = await dc.get_or_fetch("trending", "movie", 1, fetch)

    assert first == second == ["item"]
    assert calls == 1


async def test_cache_miss_after_ttl_expiry(monkeypatch, fake_clock):
    monkeypatch.setenv("DISCOVER_CACHE_TTL_SECONDS", "10")
    calls = 0

    async def fetch():
        nonlocal calls
        calls += 1
        return [f"item-{calls}"]

    await dc.get_or_fetch("trending", "movie", 1, fetch)
    fake_clock["now"] = 11
    result = await dc.get_or_fetch("trending", "movie", 1, fetch)

    assert calls == 2
    assert result == ["item-2"]


async def test_concurrent_miss_on_same_key_fetches_once(fake_clock):
    # Two "requests" for the same key racing on a cold cache should not both
    # hit the upstream fetch — the second should await the first's result.
    calls = 0
    started = asyncio.Event()

    async def slow_fetch():
        nonlocal calls
        calls += 1
        started.set()
        await asyncio.sleep(0)  # yield control so the second call can race in
        return ["item"]

    results = await asyncio.gather(
        dc.get_or_fetch("trending", "movie", 1, slow_fetch),
        dc.get_or_fetch("trending", "movie", 1, slow_fetch),
    )

    assert results == [["item"], ["item"]]
    assert calls == 1


async def test_independent_keys(fake_clock):
    async def fetch_a():
        return ["a"]

    async def fetch_b():
        return ["b"]

    ra = await dc.get_or_fetch("trending", "movie", 1, fetch_a)
    rb = await dc.get_or_fetch("popular", "movie", 1, fetch_b)
    rb_tv = await dc.get_or_fetch("trending", "tv", 1, fetch_b)
    rb_page2 = await dc.get_or_fetch("trending", "movie", 2, fetch_b)

    assert ra == ["a"]
    assert rb == rb_tv == rb_page2 == ["b"]
