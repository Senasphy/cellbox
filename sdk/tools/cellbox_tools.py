"""Bounded model tool dispatcher for a Python cellbox Session."""
from __future__ import annotations

import json
from typing import Any

CAP = 16 * 1024
MARKER = b"\n... <omitted {n} bytes> ...\n"

def _clip(value: str | bytes, cap: int = CAP) -> tuple[str, int]:
    data = value.encode() if isinstance(value, str) else value
    if len(data) <= cap:
        return data.decode("utf-8", "replace"), 0
    marker = MARKER.replace(b"{n}", str(len(data) - cap).encode())
    room = max(0, cap - len(marker))
    head = room // 2
    tail = room - head
    return (data[:head] + marker + (data[-tail:] if tail else b"")).decode("utf-8", "replace"), len(data) - head - tail

def dispatch(session: Any, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
    if name == "run_command":
        result = session.exec(arguments["command"], timeout_s=float(arguments.get("timeout_s", 60)))
        stdout, out_omitted = _clip(result.stdout)
        stderr, err_omitted = _clip(result.stderr)
        reason = "timeout" if result.state == "timed_out" else ("oom" if result.state == "oom" else None)
        return {"exit_code": result.exit_code, "duration_ms": result.duration_ms,
                "timeout_or_oom_reason": reason, "stdout": stdout, "stderr": stderr,
                "omitted_bytes": {"stdout": out_omitted, "stderr": err_omitted}}
    if name == "write_file":
        session.write_file(arguments["path"], arguments["contents"])
        return {"written": True, "path": arguments["path"]}
    if name == "read_file":
        content, omitted = _clip(session.read_file(arguments["path"]))
        return {"path": arguments["path"], "contents": content, "omitted_bytes": omitted}
    if name == "list_files":
        return {"items": session.list_files(arguments.get("path", "/"), limit=arguments.get("limit", 50))}
    if name in {"snapshot", "fork"}:
        raise NotImplementedError(f"{name} is available in Phase 6")
    raise ValueError(f"unknown cellbox tool: {name}")

def dispatch_json(session: Any, name: str, arguments: str | dict[str, Any]) -> str:
    if isinstance(arguments, str): arguments = json.loads(arguments)
    try: return json.dumps(dispatch(session, name, arguments), ensure_ascii=False)
    except Exception as exc: return json.dumps({"error": str(exc)}, ensure_ascii=False)
