# API reference

The OpenAPI specification lives in [api/openapi.yaml](../api/openapi.yaml). All routes are mounted under `/v1`, control requests take JSON, and authenticated routes expect `Authorization: Bearer <key>`. Errors use `application/problem+json` with a stable `code` field.

## Quick walkthrough

Requires a running `cellbox serve` instance (native or `--backend docker`), `curl`, and `jq`. There's also an automated version of this flow in [scripts/docker-walkthrough.sh](../scripts/docker-walkthrough.sh).

Start the server — on first run it prints the bootstrap admin key once:

```sh
cellbox serve
```

In another terminal, mint a session-scoped API key and create a session:

```sh
export CELLBOX_URL=http://127.0.0.1:8080
export CELLBOX_KEY="$(cellbox keys create agent)"

session_json=$(curl -fsS "$CELLBOX_URL/v1/sessions" \
  -H "Authorization: Bearer $CELLBOX_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"image":"python:3.12-slim","network_policy":{"mode":"none"}}')
session_id=$(printf '%s' "$session_json" | jq -r .id)
```

Run a command and stream its output over SSE:

```sh
exec_json=$(curl -fsS -X POST "$CELLBOX_URL/v1/sessions/$session_id/execs" \
  -H "Authorization: Bearer $CELLBOX_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"argv":["python","-c","print(42)"]}')
exec_id=$(printf '%s' "$exec_json" | jq -r .id)
curl -N "$CELLBOX_URL/v1/sessions/$session_id/execs/$exec_id/stream" \
  -H "Authorization: Bearer $CELLBOX_KEY"
```

Upload, list, and download a file, then clean up the session:

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

## Networking & egress

- **Docker backend**: Runs with `NetworkMode=none` and only accepts `network_policy.mode=none`.
- **Native backend**: Supports `none`, `allowlist`, and (when `egress.allow_internet_mode` is enabled) `internet`.
  - Allowlist example: `{"mode":"allowlist","allow":[{"host":"*.example.com","ports":[443]}]}` (wildcard matches subdomains, not the apex domain).
  - Outbound HTTP/HTTPS traffic goes through the session proxy at `127.0.0.1:3128` via `HTTP_PROXY` / `HTTPS_PROXY` (and lowercase equivalents); direct external interfaces are absent.
  - `GET /v1/sessions/{id}/egress-log` returns paginated allow/deny decisions.

## Terminals, snapshots, forks & replay

- **Interactive PTY & TTY exec**: Both backends support interactive WebSocket terminals (`capabilities.tty`) and `tty: true` on exec requests (`capabilities.tty_exec`). Disconnected PTYs stay alive for `terminal.grace_s` (default 30s) and replay up to 256 KiB of recent output on reconnect.
- **Signals**: `POST /v1/sessions/{id}/execs/{execID}/signal` sends a POSIX signal (e.g. `{"signal":15}`) to an active native exec. The Docker backend returns `capability_unsupported` here since Docker Engine can't signal individual exec processes.
- **Snapshots & forks**: `POST`/`GET /v1/sessions/{id}/snapshots` create and list snapshots; `GET`/`DELETE /v1/snapshots/{snapshotID}` inspect or delete them (deletion returns a conflict if child snapshots or live forks depend on it). Native snapshots freeze the session and copy its writable overlay; Docker snapshots use image commits. `POST /v1/sessions/{id}/fork` creates a new session from `snapshot_id`, or snapshots and forks the live session if omitted. Native snapshot chains flatten automatically into a standalone base at the overlay depth threshold.
- **Replay & asciicast**: `GET /v1/sessions/{id}/replay` returns paged timestamped exec boundaries and base64 output chunks. TTY execs can be exported as asciicast v2 from `/v1/sessions/{id}/execs/{execID}/asciicast`.
- **Warm pool**: When enabled, primes configured images at startup and refills after cold creates. Claims currently apply to `none` network policy sessions without snapshot layers.

## Backend limits, idempotency & pagination

- **Disk limits**: The native backend enforces `limits.disk_mb` via a 2-second polling watcher on the writable overlay. The Docker backend reports `disk_limits: false` and rejects explicit `limits.disk_mb` requests.
- **Idempotency**: Mutating `POST` routes accept an `Idempotency-Key` header (up to 255 bytes). Successful responses are cached per key for 24 hours; reusing the key with a different request returns `422 idempotency_key_reused`.
- **Pagination**: Session, exec, and directory listings take `limit` (default 50, max 200) and opaque `cursor` query parameters, returning `next_cursor` when more results exist. The Docker backend rejects directory listings over 100,000 entries.
- **Observability**: `GET /v1/sessions/{id}/events` returns paginated lifecycle audit events. `GET /metrics` exposes Prometheus metrics for sessions, capacity, execs, creation latency, output bytes, and HTTP routes.

## API keys & quotas

Admin keys can create, list, and revoke API keys; secrets are returned once on creation. New session-scoped keys default to 20 active sessions, 8192 MB aggregate memory, and 20 requests/sec. Admin keys are unmetered. Session creation checks committed CPU, memory, and PID capacity and returns `503 capacity_exceeded` with `Retry-After` when full.

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
