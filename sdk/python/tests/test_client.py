from __future__ import annotations

import asyncio
import base64

import httpx
import pytest

from cellbox import AsyncClient, CellboxError, Client, ProblemError
from cellbox.client import _async_sse_chunks, _exec_body, _same_origin_url, _sse_chunks


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


def test_snapshot_and_fork_session_methods() -> None:
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/snapshots"):
            return httpx.Response(201, json={"id": "snp_test"})
        if request.url.path.endswith("/fork"):
            return httpx.Response(201, json={"id": "ses_child", "parent_snapshot_id": "snp_test"})
        return httpx.Response(201, json={"id": "ses_base", "state": "ready"})

    client = Client("http://cellbox.test", "cbx_key", transport=httpx.MockTransport(handler))
    base = client.sessions.create("alpine")
    assert base.snapshot(labels={"branch": "base"})["id"] == "snp_test"
    child = base.fork("snp_test")
    assert child.id == "ses_child"
    assert requests[1].url.path == "/v1/sessions/ses_base/snapshots"
    assert requests[2].read() == b'{"snapshot_id":"snp_test"}'
    client.close()


def test_sync_exec_stream_surfaces_server_error_event() -> None:
    lines = iter(['event: error', 'data: {"title":"Exec failed","detail":"launch denied"}', ""])
    with pytest.raises(CellboxError, match="launch denied"):
        list(_sse_chunks(lines))


def test_sse_output_exposes_exec_id_and_resume_cursor() -> None:
    payload = '{"seq":4,"stream":"stdout","data":"aGVsbG8="}'
    chunks = list(_sse_chunks(iter(["id: 9", "event: output", f"data: {payload}", ""]), exec_id="exe_test"))
    assert len(chunks) == 1
    assert chunks[0].exec_id == "exe_test"
    assert chunks[0].sequence == 4
    assert chunks[0].cursor == 9
    assert chunks[0].data == b"hello"


def test_exec_stream_url_rejects_foreign_origin() -> None:
    with pytest.raises(CellboxError, match="outside the configured service origin"):
        _same_origin_url("https://cellbox.test", "https://attacker.test/steal")
    assert _same_origin_url("https://cellbox.test", "/v1/execs/exe_test/stream") == (
        "https://cellbox.test/v1/execs/exe_test/stream"
    )


def test_sync_resume_exec_stream_sends_existing_id_and_cursor() -> None:
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, text='id: 14\nevent: exit\ndata: {"state":"exited"}\n\n')

    client = Client("http://cellbox.test", "cbx_key", transport=httpx.MockTransport(handler))
    from cellbox.client import Session

    events = list(Session(client, {"id": "ses_test"}).resume_exec_stream("exe_existing", after=9))
    assert events == []
    assert requests[0].url.path == "/v1/sessions/ses_test/execs/exe_existing/stream"
    assert requests[0].headers["Last-Event-ID"] == "9"
    client.close()


def test_exec_body_rejects_blank_commands() -> None:
    for command in ("", "   ", []):
        with pytest.raises(ValueError, match="command must not be empty"):
            _exec_body(command, 1, None, None)


def test_async_exec_stream_surfaces_server_error_event() -> None:
    async def lines():
        for line in ['event: error', 'data: {"title":"Exec failed","detail":"launch denied"}', ""]:
            yield line

    async def run() -> None:
        with pytest.raises(CellboxError, match="launch denied"):
            async for _ in _async_sse_chunks(lines()):
                pass

    asyncio.run(run())


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
