"""Typed exceptions returned by the cellbox API."""

from __future__ import annotations

from typing import Any


class CellboxError(Exception):
    """Base class for client and transport errors."""


class ProblemError(CellboxError):
    """An RFC 9457 API error with its stable machine-readable code."""

    def __init__(self, status: int, code: str, title: str, detail: str = "") -> None:
        self.status = status
        self.code = code
        self.title = title
        self.detail = detail
        super().__init__(f"{code}: {detail or title} (HTTP {status})")

    @classmethod
    def from_response(cls, status: int, body: Any) -> "ProblemError":
        if not isinstance(body, dict):
            return cls(status, "http_error", "HTTP request failed")
        return cls(
            status,
            str(body.get("code", "http_error")),
            str(body.get("title", "HTTP request failed")),
            str(body.get("detail", "")),
        )
