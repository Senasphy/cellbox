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

The current Docker backend forces `NetworkMode=none`; only `network_policy.mode=none` is accepted. It reports `disk_limits: false`, returns `disk_mb: 0` for sessions without an explicit disk request, and rejects explicit `limits.disk_mb` requests because Docker quota support depends on daemon storage configuration. Interactive terminals, idempotency keys, pagination cursors, egress allowlists, quotas, and the dashboard are not implemented yet.

## API key management

An admin key can create and list API keys, and revoke a key by ID. The full secret is returned only by the create call; list responses contain key metadata without the digest or secret.

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
