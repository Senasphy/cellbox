"""Synchronous and asynchronous cellbox clients."""

from __future__ import annotations

import base64
import json
import math
import uuid
from collections.abc import AsyncIterator, Iterator, Mapping, Sequence
from dataclasses import dataclass
from datetime import datetime
from typing import Any
from urllib.parse import quote, urljoin, urlsplit

import httpx

from .errors import CellboxError, ProblemError


@dataclass(frozen=True, slots=True)
class OutputChunk:
    stream: str
    data: bytes
    sequence: int = 0
    cursor: int = 0
    exec_id: str = ""

    @property
    def text(self) -> str:
        return self.data.decode("utf-8", errors="replace")


@dataclass(frozen=True, slots=True)
class ExecResult:
    id: str
    state: str
    exit_code: int | None
    signal: int | None
    duration_ms: int
    stdout: str
    stderr: str
    output_bytes: int
    truncated: bool


def _problem(response: httpx.Response) -> ProblemError:
    try:
        body = response.json()
    except ValueError:
        body = None
    return ProblemError.from_response(response.status_code, body)


def _expect_json(response: httpx.Response) -> Any:
    if response.is_error:
        raise _problem(response)
    if response.status_code == 204:
        return None
    try:
        return response.json()
    except ValueError as exc:
        raise CellboxError("cellbox returned an invalid JSON response") from exc


def _headers(api_key: str, idempotency_key: bool = False) -> dict[str, str]:
    result = {"Authorization": f"Bearer {api_key}"}
    if idempotency_key:
        result["Idempotency-Key"] = str(uuid.uuid4())
    return result


def _same_origin_url(base_url: str, value: str) -> str:
    target = urljoin(base_url.rstrip("/") + "/", value)
    base, stream = urlsplit(base_url), urlsplit(target)
    base_scheme, stream_scheme = base.scheme.lower(), stream.scheme.lower()
    base_port = base.port or (443 if base_scheme == "https" else 80 if base_scheme == "http" else None)
    stream_port = stream.port or (443 if stream_scheme == "https" else 80 if stream_scheme == "http" else None)
    if (stream_scheme, stream.hostname, stream_port) != (base_scheme, base.hostname, base_port):
        raise CellboxError("exec stream URL is outside the configured service origin")
    return target


class Client:
    """Synchronous API client. Sessions created with ``with`` are always destroyed."""

    def __init__(
        self,
        base_url: str = "http://127.0.0.1:8080",
        api_key: str = "",
        *,
        timeout: float = 30.0,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        if not api_key:
            raise ValueError("api_key is required")
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self._http = httpx.Client(base_url=self.base_url, timeout=timeout, transport=transport)
        self.sessions = SessionManager(self)

    def _request(self, method: str, path: str, **kwargs: Any) -> Any:
        headers = dict(kwargs.pop("headers", {}))
        headers.setdefault("Authorization", f"Bearer {self.api_key}")
        if method == "POST":
            headers.setdefault("Idempotency-Key", str(uuid.uuid4()))
        request = dict(kwargs)
        request["headers"] = headers
        for attempt in range(2):
            try:
                response = self._http.request(method, path, **request)
                return _expect_json(response)
            except httpx.TransportError:
                if attempt:
                    raise
        raise AssertionError("unreachable")

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> "Client":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()


class SessionManager:
    def __init__(self, client: Client) -> None:
        self._client = client

    def create(
        self,
        image: str,
        *,
        cpu_millicores: int | None = None,
        memory_mb: int | None = None,
        pids: int | None = None,
        disk_mb: int | None = None,
        ttl_s: int | None = None,
        idle_ttl_s: int | None = None,
        env: Mapping[str, str] | None = None,
        labels: Mapping[str, str] | None = None,
        network_policy: Mapping[str, Any] | None = None,
    ) -> "Session":
        limits = {
            key: value
            for key, value in {
                "cpu_millicores": cpu_millicores,
                "memory_mb": memory_mb,
                "pids": pids,
                "disk_mb": disk_mb,
                "ttl_s": ttl_s,
                "idle_ttl_s": idle_ttl_s,
            }.items()
            if value is not None
        }
        body: dict[str, Any] = {"image": image}
        if limits:
            body["limits"] = limits
        if env is not None:
            body["env"] = dict(env)
        if labels is not None:
            body["labels"] = dict(labels)
        if network_policy is not None:
            body["network_policy"] = dict(network_policy)
        response = self._client._request("POST", "/v1/sessions", json=body)
        return Session(self._client, response)

    def get(self, session_id: str) -> "Session":
        response = self._client._request("GET", f"/v1/sessions/{quote(session_id, safe='')}")
        return Session(self._client, response)

    def list(self, *, limit: int = 50) -> list[dict[str, Any]]:
        response = self._client._request("GET", "/v1/sessions", params={"limit": limit})
        return list(response.get("items", []))


class Session:
    def __init__(self, client: Client, data: Mapping[str, Any]) -> None:
        self._client = client
        self.id = str(data["id"])
        self.state = str(data.get("state", ""))
        self.image = str(data.get("image", ""))
        self.data = dict(data)
        self._destroyed = False

    def snapshot(self, *, labels: Mapping[str, str] | None = None) -> dict[str, Any]:
        body = {"labels": dict(labels)} if labels is not None else None
        return self._client._request("POST", f"/v1/sessions/{quote(self.id, safe='')}/snapshots", json=body)

    def fork(self, snapshot_id: str | None = None) -> "Session":
        body = {"snapshot_id": snapshot_id} if snapshot_id else None
        response = self._client._request("POST", f"/v1/sessions/{quote(self.id, safe='')}/fork", json=body)
        return Session(self._client, response)

    def exec(
        self,
        command: Sequence[str] | str,
        *,
        timeout_s: float = 60,
        cwd: str | None = None,
        env: Mapping[str, str] | None = None,
    ) -> ExecResult:
        body = _exec_body(command, timeout_s, cwd, env)
        response = self._client._request(
            "POST", f"/v1/sessions/{quote(self.id, safe='')}/execs?wait=true", json=body
        )
        return _exec_result(response)

    def exec_stream(
        self,
        command: Sequence[str] | str,
        *,
        timeout_s: float = 60,
        cwd: str | None = None,
        env: Mapping[str, str] | None = None,
    ) -> Iterator[OutputChunk]:
        body = _exec_body(command, timeout_s, cwd, env)
        started = self._client._request(
            "POST", f"/v1/sessions/{quote(self.id, safe='')}/execs", json=body
        )
        stream_path = str(started.get("stream_url", ""))
        exec_id = str(started.get("id", ""))
        if not stream_path or not exec_id:
            raise CellboxError("exec response did not include id or stream_url")
        headers = _headers(self._client.api_key)
        with self._client._http.stream(
            "GET", _same_origin_url(self._client.base_url, stream_path), headers=headers
        ) as response:
            if response.is_error:
                raise _problem(response)
            yield from _sse_chunks(response.iter_lines(), exec_id=exec_id)

    def resume_exec_stream(self, exec_id: str, *, after: int = 0) -> Iterator[OutputChunk]:
        """Follow an existing exec after its last received byte cursor."""
        path = f"/v1/sessions/{quote(self.id, safe='')}/execs/{quote(exec_id, safe='')}/stream"
        headers = _headers(self._client.api_key)
        if after > 0:
            headers["Last-Event-ID"] = str(after)
        with self._client._http.stream("GET", path, headers=headers) as response:
            if response.is_error:
                raise _problem(response)
            yield from _sse_chunks(response.iter_lines(), exec_id=exec_id)

    def read_file(self, path: str) -> bytes:
        response = self._client._http.get(
            f"/v1/sessions/{quote(self.id, safe='')}/files",
            params={"path": path},
            headers=_headers(self._client.api_key),
        )
        if response.is_error:
            raise _problem(response)
        return response.content

    def write_file(self, path: str, contents: str | bytes) -> None:
        payload = contents.encode("utf-8") if isinstance(contents, str) else contents
        response = self._client._http.put(
            f"/v1/sessions/{quote(self.id, safe='')}/files",
            params={"path": path},
            content=payload,
            headers={**_headers(self._client.api_key), "Content-Type": "application/octet-stream"},
        )
        if response.is_error:
            raise _problem(response)

    def list_files(self, path: str = "/", *, limit: int = 50) -> list[dict[str, Any]]:
        query: dict[str, Any] = {"path": path, "limit": limit}
        result: list[dict[str, Any]] = []
        while True:
            page = self._client._request(
                "GET", f"/v1/sessions/{quote(self.id, safe='')}/files/list", params=query
            )
            result.extend(page.get("items", []))
            cursor = page.get("next_cursor")
            if not cursor:
                return result
            query["cursor"] = cursor

    def destroy(self) -> None:
        if self._destroyed:
            return
        response = self._client._http.delete(
            f"/v1/sessions/{quote(self.id, safe='')}", headers=_headers(self._client.api_key)
        )
        if response.status_code == 404:
            self._destroyed = True
            return
        if response.is_error:
            raise _problem(response)
        self._destroyed = True

    def __enter__(self) -> "Session":
        return self

    def __exit__(self, *_: object) -> None:
        self.destroy()


class AsyncClient:
    """Async API client with async context managers for clients and sessions."""

    def __init__(
        self,
        base_url: str = "http://127.0.0.1:8080",
        api_key: str = "",
        *,
        timeout: float = 30.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        if not api_key:
            raise ValueError("api_key is required")
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self._http = httpx.AsyncClient(base_url=self.base_url, timeout=timeout, transport=transport)
        self.sessions = AsyncSessionManager(self)

    async def _request(self, method: str, path: str, **kwargs: Any) -> Any:
        headers = dict(kwargs.pop("headers", {}))
        headers.setdefault("Authorization", f"Bearer {self.api_key}")
        if method == "POST":
            headers.setdefault("Idempotency-Key", str(uuid.uuid4()))
        request = dict(kwargs)
        request["headers"] = headers
        for attempt in range(2):
            try:
                response = await self._http.request(method, path, **request)
                return _expect_json(response)
            except httpx.TransportError:
                if attempt:
                    raise
        raise AssertionError("unreachable")

    async def aclose(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "AsyncClient":
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()


class AsyncSessionManager:
    def __init__(self, client: AsyncClient) -> None:
        self._client = client

    async def create(
        self,
        image: str,
        *,
        cpu_millicores: int | None = None,
        memory_mb: int | None = None,
        pids: int | None = None,
        disk_mb: int | None = None,
        ttl_s: int | None = None,
        idle_ttl_s: int | None = None,
        env: Mapping[str, str] | None = None,
        labels: Mapping[str, str] | None = None,
        network_policy: Mapping[str, Any] | None = None,
    ) -> "AsyncSession":
        body: dict[str, Any] = {"image": image}
        limits = {
            key: value
            for key, value in {
                "cpu_millicores": cpu_millicores,
                "memory_mb": memory_mb,
                "pids": pids,
                "disk_mb": disk_mb,
                "ttl_s": ttl_s,
                "idle_ttl_s": idle_ttl_s,
            }.items()
            if value is not None
        }
        if limits:
            body["limits"] = limits
        if env is not None:
            body["env"] = dict(env)
        if labels is not None:
            body["labels"] = dict(labels)
        if network_policy is not None:
            body["network_policy"] = dict(network_policy)
        return AsyncSession(self._client, await self._client._request("POST", "/v1/sessions", json=body))

    async def get(self, session_id: str) -> "AsyncSession":
        data = await self._client._request("GET", f"/v1/sessions/{quote(session_id, safe='')}")
        return AsyncSession(self._client, data)

    async def list(self, *, limit: int = 50) -> list[dict[str, Any]]:
        response = await self._client._request("GET", "/v1/sessions", params={"limit": limit})
        return list(response.get("items", []))


class AsyncSession:
    def __init__(self, client: AsyncClient, data: Mapping[str, Any]) -> None:
        self._client = client
        self.id = str(data["id"])
        self.state = str(data.get("state", ""))
        self.image = str(data.get("image", ""))
        self.data = dict(data)
        self._destroyed = False

    async def snapshot(self, *, labels: Mapping[str, str] | None = None) -> dict[str, Any]:
        body = {"labels": dict(labels)} if labels is not None else None
        return await self._client._request("POST", f"/v1/sessions/{quote(self.id, safe='')}/snapshots", json=body)

    async def fork(self, snapshot_id: str | None = None) -> "AsyncSession":
        body = {"snapshot_id": snapshot_id} if snapshot_id else None
        response = await self._client._request("POST", f"/v1/sessions/{quote(self.id, safe='')}/fork", json=body)
        return AsyncSession(self._client, response)

    async def exec(
        self,
        command: Sequence[str] | str,
        *,
        timeout_s: float = 60,
        cwd: str | None = None,
        env: Mapping[str, str] | None = None,
    ) -> ExecResult:
        response = await self._client._request(
            "POST",
            f"/v1/sessions/{quote(self.id, safe='')}/execs?wait=true",
            json=_exec_body(command, timeout_s, cwd, env),
        )
        return _exec_result(response)

    async def exec_stream(
        self,
        command: Sequence[str] | str,
        *,
        timeout_s: float = 60,
        cwd: str | None = None,
        env: Mapping[str, str] | None = None,
    ) -> AsyncIterator[OutputChunk]:
        started = await self._client._request(
            "POST",
            f"/v1/sessions/{quote(self.id, safe='')}/execs",
            json=_exec_body(command, timeout_s, cwd, env),
        )
        stream_path = str(started.get("stream_url", ""))
        exec_id = str(started.get("id", ""))
        if not stream_path or not exec_id:
            raise CellboxError("exec response did not include id or stream_url")
        async with self._client._http.stream(
            "GET", _same_origin_url(self._client.base_url, stream_path), headers=_headers(self._client.api_key)
        ) as response:
            if response.is_error:
                raise _problem(response)
            async for chunk in _async_sse_chunks(response.aiter_lines(), exec_id=exec_id):
                yield chunk

    async def resume_exec_stream(
        self, exec_id: str, *, after: int = 0
    ) -> AsyncIterator[OutputChunk]:
        """Follow an existing exec after its last received byte cursor."""
        path = f"/v1/sessions/{quote(self.id, safe='')}/execs/{quote(exec_id, safe='')}/stream"
        headers = _headers(self._client.api_key)
        if after > 0:
            headers["Last-Event-ID"] = str(after)
        async with self._client._http.stream("GET", path, headers=headers) as response:
            if response.is_error:
                raise _problem(response)
            async for chunk in _async_sse_chunks(response.aiter_lines(), exec_id=exec_id):
                yield chunk

    async def read_file(self, path: str) -> bytes:
        response = await self._client._http.get(
            f"/v1/sessions/{quote(self.id, safe='')}/files",
            params={"path": path},
            headers=_headers(self._client.api_key),
        )
        if response.is_error:
            raise _problem(response)
        return response.content

    async def write_file(self, path: str, contents: str | bytes) -> None:
        payload = contents.encode("utf-8") if isinstance(contents, str) else contents
        response = await self._client._http.put(
            f"/v1/sessions/{quote(self.id, safe='')}/files",
            params={"path": path},
            content=payload,
            headers={**_headers(self._client.api_key), "Content-Type": "application/octet-stream"},
        )
        if response.is_error:
            raise _problem(response)

    async def list_files(self, path: str = "/", *, limit: int = 50) -> list[dict[str, Any]]:
        query: dict[str, Any] = {"path": path, "limit": limit}
        result: list[dict[str, Any]] = []
        while True:
            page = await self._client._request(
                "GET", f"/v1/sessions/{quote(self.id, safe='')}/files/list", params=query
            )
            result.extend(page.get("items", []))
            cursor = page.get("next_cursor")
            if not cursor:
                return result
            query["cursor"] = cursor

    async def destroy(self) -> None:
        if self._destroyed:
            return
        response = await self._client._http.delete(
            f"/v1/sessions/{quote(self.id, safe='')}", headers=_headers(self._client.api_key)
        )
        if response.status_code == 404:
            self._destroyed = True
            return
        if response.is_error:
            raise _problem(response)
        self._destroyed = True

    async def __aenter__(self) -> "AsyncSession":
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.destroy()


def _exec_body(
    command: Sequence[str] | str,
    timeout_s: float,
    cwd: str | None,
    env: Mapping[str, str] | None,
) -> dict[str, Any]:
    if not math.isfinite(timeout_s) or timeout_s <= 0:
        raise ValueError("timeout_s must be a finite positive number")
    body: dict[str, Any]
    if isinstance(command, str):
        if not command.strip():
            raise ValueError("command must not be empty")
        body = {"shell": command}
    else:
        if not command:
            raise ValueError("command must not be empty")
        body = {"argv": list(command)}
    body["timeout_ms"] = max(1, round(timeout_s * 1000))
    if cwd is not None:
        body["cwd"] = cwd
    if env is not None:
        body["env"] = dict(env)
    return body


def _exec_result(response: Mapping[str, Any]) -> ExecResult:
    execution = response.get("exec", {})
    output = response.get("output", [])
    stdout = bytearray()
    stderr = bytearray()
    for event in output:
        try:
            data = base64.b64decode(event.get("data", ""), validate=True)
        except (ValueError, TypeError) as exc:
            raise CellboxError("cellbox returned invalid base64 output") from exc
        if event.get("stream") == "stderr":
            stderr.extend(data)
        else:
            stdout.extend(data)
    duration_ms = execution.get("duration_ms")
    if duration_ms is None:
        started_at = execution.get("started_at")
        ended_at = execution.get("ended_at")
        if started_at and ended_at:
            try:
                start = datetime.fromisoformat(str(started_at).replace("Z", "+00:00"))
                end = datetime.fromisoformat(str(ended_at).replace("Z", "+00:00"))
                duration_ms = max(0, round((end - start).total_seconds() * 1000))
            except ValueError:
                duration_ms = 0
        else:
            duration_ms = 0
    return ExecResult(
        id=str(execution.get("id", "")),
        state=str(execution.get("state", "")),
        exit_code=execution.get("exit_code"),
        signal=execution.get("signal"),
        duration_ms=int(duration_ms),
        stdout=stdout.decode("utf-8", errors="replace"),
        stderr=stderr.decode("utf-8", errors="replace"),
        output_bytes=int(execution.get("output_bytes", len(stdout) + len(stderr))),
        truncated=bool(execution.get("truncated", False)),
    )


def _sse_chunks(lines: Iterator[str], *, exec_id: str = "") -> Iterator[OutputChunk]:
    event_name = ""
    data_lines: list[str] = []
    cursor = 0
    for line in lines:
        if not line:
            if event_name == "output" and data_lines:
                yield _decode_output_event("\n".join(data_lines), cursor, exec_id)
            elif event_name == "error" and data_lines:
                raise _decode_sse_error("\n".join(data_lines))
            event_name, data_lines, cursor = "", [], 0
            continue
        if line.startswith("event:"):
            event_name = line.removeprefix("event:").strip()
        elif line.startswith("data:"):
            data_lines.append(line.removeprefix("data:").lstrip())
        elif line.startswith("id:"):
            try:
                cursor = int(line.removeprefix("id:").strip())
            except ValueError as exc:
                raise CellboxError("cellbox returned an invalid SSE event cursor") from exc
    if event_name == "output" and data_lines:
        yield _decode_output_event("\n".join(data_lines), cursor, exec_id)
    elif event_name == "error" and data_lines:
        raise _decode_sse_error("\n".join(data_lines))


async def _async_sse_chunks(lines: AsyncIterator[str], *, exec_id: str = "") -> AsyncIterator[OutputChunk]:
    event_name = ""
    data_lines: list[str] = []
    cursor = 0
    async for line in lines:
        if not line:
            if event_name == "output" and data_lines:
                yield _decode_output_event("\n".join(data_lines), cursor, exec_id)
            elif event_name == "error" and data_lines:
                raise _decode_sse_error("\n".join(data_lines))
            event_name, data_lines, cursor = "", [], 0
            continue
        if line.startswith("event:"):
            event_name = line.removeprefix("event:").strip()
        elif line.startswith("data:"):
            data_lines.append(line.removeprefix("data:").lstrip())
        elif line.startswith("id:"):
            try:
                cursor = int(line.removeprefix("id:").strip())
            except ValueError as exc:
                raise CellboxError("cellbox returned an invalid SSE event cursor") from exc
    if event_name == "output" and data_lines:
        yield _decode_output_event("\n".join(data_lines), cursor, exec_id)
    elif event_name == "error" and data_lines:
        raise _decode_sse_error("\n".join(data_lines))


def _decode_output_event(raw: str, cursor: int = 0, exec_id: str = "") -> OutputChunk:
    try:
        event = json.loads(raw)
        data = base64.b64decode(event["data"], validate=True)
        sequence = int(event.get("seq", 0))
        return OutputChunk(
            str(event.get("stream", "stdout")),
            data,
            sequence,
            cursor or sequence + len(data),
            exec_id,
        )
    except (ValueError, KeyError, TypeError) as exc:
        raise CellboxError("cellbox returned an invalid output event") from exc


def _decode_sse_error(raw: str) -> CellboxError:
    try:
        payload = json.loads(raw)
        detail = payload.get("detail") or payload.get("message") or payload.get("title") or "exec stream failed"
        return CellboxError(str(detail))
    except (ValueError, TypeError, AttributeError) as exc:
        raise CellboxError("cellbox returned an invalid exec error event") from exc
