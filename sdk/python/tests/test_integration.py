from __future__ import annotations

import os

import pytest

from cellbox import Client


@pytest.mark.integration
def test_python_sdk_against_live_docker_server() -> None:
    url = os.environ.get("CELLBOX_URL")
    key = os.environ.get("CELLBOX_KEY")
    if not url or not key:
        pytest.skip("set CELLBOX_URL and CELLBOX_KEY to run the live Docker integration")
    with Client(url, key) as client:
        with client.sessions.create("alpine:latest", network_policy={"mode": "none"}) as session:
            session.write_file("/tmp/python-sdk.txt", "created by the SDK\n")
            assert session.read_file("/tmp/python-sdk.txt") == b"created by the SDK\n"
            assert any(item["path"] == "/tmp/python-sdk.txt" for item in session.list_files("/tmp"))
            result = session.exec(["/bin/sh", "-lc", "cat /tmp/python-sdk.txt"])
            assert result.exit_code == 0
            assert result.stdout == "created by the SDK\n"
            chunks = list(session.exec_stream(["/bin/sh", "-lc", "printf sdk-stream-ok"]))
            assert b"".join(chunk.data for chunk in chunks) == b"sdk-stream-ok"
