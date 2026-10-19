# API reference

The current contract is in [api/openapi.yaml](../api/openapi.yaml). All routes are mounted under `/v1`; send JSON for control requests and `Authorization: Bearer <key>` for authenticated routes. Errors use `application/problem+json` with a stable `code`.

## Docker-backed walkthrough

This walkthrough covers the current Phase 1 surface. It requires a reachable Docker Engine socket, a Linux host, `curl`, and `jq`. The Docker backend is a bootstrap backend, and the full sequence has not yet been verified against a live Docker daemon.

The repository includes an automated version of this flow at `scripts/docker-walkthrough.sh`. After building `bin/cellbox`, run it on a Docker-enabled host. `make test-integration` runs the live Docker runtime contract and fails if the daemon is unavailable. CI runs both checks on its Ubuntu runner; a local pass against a stub is not a substitute for that live-daemon result.

Start the service. On its first run it prints the admin key once; keep that value for local use.

```sh
cellbox serve --backend docker
```

In another terminal, create an API key and a session:

```sh
export CELLBOX_URL=http://127.0.0.1:8080
export CELLBOX_KEY="$(cellbox keys create agent)"

session_json=$(curl -fsS "$CELLBOX_URL/v1/sessions" \
  -H "Authorization: Bearer $CELLBOX_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"image":"python:3.12-slim","network_policy":{"mode":"none"}}')
session_id=$(printf '%s' "$session_json" | jq -r .id)
```

Start a command and attach to its resumable server-sent event stream:

```sh
exec_json=$(curl -fsS -X POST "$CELLBOX_URL/v1/sessions/$session_id/execs" \
  -H "Authorization: Bearer $CELLBOX_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"argv":["python","-c","print(42)"]}')
exec_id=$(printf '%s' "$exec_json" | jq -r .id)
curl -N "$CELLBOX_URL/v1/sessions/$session_id/execs/$exec_id/stream" \
  -H "Authorization: Bearer $CELLBOX_KEY"
```

Write, list, and download a file, then destroy the session:

```sh
printf 'hello from cellbox\n' > hello.txt
curl -fsS -X PUT "$CELLBOX_URL/v1/sessions/$session_id/files?path=%2Ftmp%2Fhello.txt" \
  -H "Authorization: Bearer $CELLBOX_KEY" \
  -H 'Content-Type: application/octet-stream' \
  --data-binary @hello.txt
curl -fsS "$CELLBOX_URL/v1/sessions/$session_id/files/list?path=%2Ftmp" \
  -H "Authorization: Bearer $CELLBOX_KEY"
curl -fsS "$CELLBOX_URL/v1/sessions/$session_id/files?path=%2Ftmp%2Fhello.txt" \
  -H "Authorization: Bearer $CELLBOX_KEY" -o downloaded.txt
curl -fsS -X DELETE "$CELLBOX_URL/v1/sessions/$session_id" \
  -H "Authorization: Bearer $CELLBOX_KEY"
```

The current Docker backend forces `NetworkMode=none`; only `network_policy.mode=none` is accepted. It reports `disk_limits: false`, returns `disk_mb: 0` for sessions without an explicit disk request, and rejects explicit `limits.disk_mb` requests because Docker quota support depends on daemon storage configuration. Interactive terminals, egress allowlists, and the dashboard are not implemented yet.

Mutating POST requests accept an optional `Idempotency-Key` header (up to 255 bytes). Successful responses are retained for 24 hours per authenticated API key. Repeating the same method, path, query, key, and body returns the stored status, headers, and response body. Reusing the key with a different request returns `422 idempotency_key_reused`. Errors are not cached.

Session, exec, and directory listing routes accept `limit` and opaque `cursor` query parameters. The default page size is 50 and the maximum is 200. Sessions are ordered newest first, execs oldest first, and directory entries by path. Responses include `next_cursor` only when another page is available. The Docker adapter rejects directory listings larger than 100,000 entries.

An active exec can receive a POSIX signal with `POST /v1/sessions/{session_id}/execs/{exec_id}/signal` and a JSON body such as `{"signal":15}`. The Docker backend reports `capability_unsupported` because Docker Engine cannot signal one exec process independently.

## API key management

An admin key can create and list API keys, and revoke a key by ID. The full secret is returned only by the create call; list responses contain key metadata without the digest or secret.

New session-scoped keys receive quotas of 20 active sessions, 8192 MB aggregate memory, and 20 requests per second. Admin keys are unmetered. Session creation also checks committed CPU, memory, and PID capacity using the host or enclosing cgroup limits; when capacity is exhausted it returns `503 capacity_exceeded` and `Retry-After`.

```sh
created_key=$(curl -fsS -X POST "$CELLBOX_URL/v1/keys" \
  -H "Authorization: Bearer $CELLBOX_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"name":"agent","scope":"session"}')
new_key=$(printf '%s' "$created_key" | jq -r .secret)
curl -fsS "$CELLBOX_URL/v1/keys" -H "Authorization: Bearer $CELLBOX_KEY"
key_id=$(printf '%s' "$created_key" | jq -r .id)
curl -fsS -X DELETE "$CELLBOX_URL/v1/keys/$key_id" \
  -H "Authorization: Bearer $CELLBOX_KEY"
```
