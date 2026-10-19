from __future__ import annotations

import asyncio
import base64

import httpx
import pytest

from cellbox import AsyncClient, Client, ProblemError


def test_context_manager_creates_and_destroys_session() -> None:
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            return httpx.Response(201, json={"id": "ses_test", "state": "ready"})
        return httpx.Response(204)

    with Client("http://cellbox.test", "cbx_key", transport=httpx.MockTransport(handler)) as client:
        with client.sessions.create("alpine:latest", memory_mb=256) as session:
            assert session.id == "ses_test"
            assert session.state == "ready"

    assert [request.method for request in requests] == ["POST", "DELETE"]
    assert requests[0].headers["Authorization"] == "Bearer cbx_key"
    assert requests[0].headers["Idempotency-Key"]
    assert requests[1].url.path == "/v1/sessions/ses_test"


def test_exec_result_decodes_separate_streams() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.query == b"wait=true"
        return httpx.Response(
            200,
            json={
                "exec": {"id": "exe_test", "state": "exited", "exit_code": 0, "output_bytes": 8},
                "output": [
                    {"stream": "stdout", "data": base64.b64encode(b"hello\n").decode()},
                    {"stream": "stderr", "data": base64.b64encode(b"warn").decode()},
                ],
            },
        )

    client = Client("http://cellbox.test", "cbx_key", transport=httpx.MockTransport(handler))
    from cellbox.client import Session

    result = Session(client, {"id": "ses_test"}).exec(["echo", "hello"])
    assert result.stdout == "hello\n"
    assert result.stderr == "warn"
    assert result.exit_code == 0
    client.close()


def test_problem_code_is_available_on_typed_exception() -> None:
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(
            422,
            json={"title": "Invalid limits", "status": 422, "code": "limit_invalid", "detail": "too much"},
        )

    client = Client("http://cellbox.test", "cbx_key", transport=httpx.MockTransport(handler))
    with pytest.raises(ProblemError) as raised:
        client.sessions.create("alpine")
    assert raised.value.code == "limit_invalid"
    assert raised.value.status == 422
    client.close()


def test_post_retries_reuse_idempotency_key() -> None:
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if len(requests) == 1:
            raise httpx.ConnectError("connection dropped", request=request)
        return httpx.Response(201, json={"id": "ses_retry", "state": "ready"})

    client = Client("http://cellbox.test", "cbx_key", transport=httpx.MockTransport(handler))
    assert client.sessions.create("alpine").id == "ses_retry"
    assert len(requests) == 2
    assert requests[0].headers["Idempotency-Key"] == requests[1].headers["Idempotency-Key"]
    client.close()


def test_async_context_manager_creates_and_destroys_session() -> None:
    requests: list[httpx.Request] = []

    async def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.method == "POST":
            return httpx.Response(201, json={"id": "ses_async", "state": "ready"})
        return httpx.Response(204)

    async def run() -> None:
        async with AsyncClient(
            "http://cellbox.test", "cbx_key", transport=httpx.MockTransport(handler)
        ) as client:
            async with await client.sessions.create("alpine:latest") as session:
                assert session.id == "ses_async"

    asyncio.run(run())
    assert [request.method for request in requests] == ["POST", "DELETE"]
