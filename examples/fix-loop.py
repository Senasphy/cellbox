"""Iteratively fix a failing sandboxed test with a fake or hosted model."""
from __future__ import annotations

import argparse
import json
import os
import re
import urllib.request

from cellbox import Client


def suggest_fix(source: str, failure: str, *, fake_model: bool) -> str:
    if fake_model:
        return "def add(a, b):\n    return a + b\n"
    endpoint = os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1").rstrip("/")
    request = urllib.request.Request(
        endpoint + "/chat/completions",
        data=json.dumps({
            "model": os.getenv("OPENAI_MODEL", "gpt-4o-mini"),
            "messages": [
                {"role": "system", "content": "Fix the Python code so the reported test passes. Return only the complete corrected source file, without markdown fences."},
                {"role": "user", "content": f"Source:\n```python\n{source}\n```\n\nTest failure:\n{failure}"},
            ],
            "temperature": 0,
        }).encode(),
        headers={"Authorization": f"Bearer {os.environ['OPENAI_API_KEY']}", "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=120) as response:
        content = json.load(response)["choices"][0]["message"].get("content", "")
    match = re.fullmatch(r"\s*```(?:python)?\s*\n?(.*?)\n?```\s*", content, re.DOTALL)
    return match.group(1) if match else content


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fake-model", action="store_true", help="use a deterministic offline repair")
    parser.add_argument("--max-attempts", type=int, default=3)
    args = parser.parse_args()
    if args.max_attempts < 1:
        parser.error("--max-attempts must be positive")
    if not args.fake_model and not os.getenv("OPENAI_API_KEY"):
        parser.error("set OPENAI_API_KEY or pass --fake-model")

    source = "def add(a, b):\n    return a - b\n"
    test = "from app import add; assert add(2, 3) == 5, f'expected 5, got {add(2, 3)}'"
    with Client(os.getenv("CELLBOX_URL", "http://127.0.0.1:8080"), os.environ["CELLBOX_KEY"]) as client:
        with client.sessions.create("python:3.12-slim", memory_mb=512, network_policy={"mode": "none"}) as box:
            box.write_file("/work/app.py", source)
            for attempt in range(1, args.max_attempts + 1):
                result = box.exec(["python", "-c", test], cwd="/work", timeout_s=30)
                print(f"attempt {attempt}: exit={result.exit_code}\n{result.stdout}{result.stderr}")
                if result.exit_code == 0:
                    return 0
                if attempt < args.max_attempts:
                    source = suggest_fix(source, result.stdout + result.stderr, fake_model=args.fake_model)
                    box.write_file("/work/app.py", source)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
