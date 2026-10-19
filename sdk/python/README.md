# Python SDK

Install with `python -m pip install cellbox-sdk` (or `pip install -e 'sdk/python[test]'` from this repository). Both `Client` and `AsyncClient` accept a base URL and API key. Session objects support exec, SSE output iteration, file transfer, directory listing, and destroy; context managers always destroy sessions.

```python
from cellbox import Client

with Client("http://127.0.0.1:8080", api_key="cbx_…") as client:
    with client.sessions.create("python:3.12-slim", memory_mb=512) as box:
        box.write_file("/work/main.py", "print(2 + 2)\n")
        result = box.exec(["python", "/work/main.py"])
        print(result.stdout, result.exit_code)
```

POST requests receive an idempotency key that is reused for one transport retry. `ProblemError` exposes the API status and problem code. Run SDK unit tests with `pytest sdk/python/tests -m 'not integration'`; live API coverage is marked `integration` and needs `CELLBOX_URL` and `CELLBOX_KEY`.
