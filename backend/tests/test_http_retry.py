import httpx
import pytest
import respx

from app.services.http import get_with_retry


@pytest.fixture(autouse=True)
def fast_retry(monkeypatch):
    # Zero backoff/jitter so retries are instant in tests.
    monkeypatch.setenv("EXTERNAL_MAX_ATTEMPTS", "3")
    monkeypatch.setenv("EXTERNAL_BACKOFF_BASE", "0")
    monkeypatch.setenv("EXTERNAL_BACKOFF_CAP", "0")
    monkeypatch.setenv("EXTERNAL_BACKOFF_JITTER", "0")
    monkeypatch.setenv("EXTERNAL_RETRY_DEADLINE", "5")


@respx.mock
async def test_retries_transient_status_then_succeeds():
    route = respx.get("https://x.test/a").mock(
        side_effect=[httpx.Response(503), httpx.Response(200, json={"ok": True})]
    )
    async with httpx.AsyncClient() as c:
        resp = await get_with_retry(c, "https://x.test/a")
    assert resp.status_code == 200
    assert route.call_count == 2


@respx.mock
async def test_retries_on_transport_error_then_succeeds():
    route = respx.get("https://x.test/t").mock(
        side_effect=[httpx.ConnectError("boom"), httpx.Response(200)]
    )
    async with httpx.AsyncClient() as c:
        resp = await get_with_retry(c, "https://x.test/t")
    assert resp.status_code == 200
    assert route.call_count == 2


@respx.mock
async def test_exhausts_and_raises_status_error():
    route = respx.get("https://x.test/b").mock(return_value=httpx.Response(500))
    async with httpx.AsyncClient() as c:
        with pytest.raises(httpx.HTTPStatusError):
            await get_with_retry(c, "https://x.test/b")
    assert route.call_count == 3  # max_attempts


@respx.mock
async def test_no_retry_on_non_transient_status():
    route = respx.get("https://x.test/c").mock(return_value=httpx.Response(404))
    async with httpx.AsyncClient() as c:
        resp = await get_with_retry(c, "https://x.test/c")
    assert resp.status_code == 404
    assert route.call_count == 1  # 404 is not retried
