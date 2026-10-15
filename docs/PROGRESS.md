# Build progress

## Phase 0: Foundations — complete (local acceptance passed)

Implemented: Go module, CLI skeleton (`version`, `doctor`, `serve`), strict config loading with environment overrides, slog redaction, SQLite WAL store and forward-only migration runner, problem+json type, prefixed sortable IDs, OpenAPI skeleton, Makefile, CI workflow, Apache-2.0 license.

Acceptance status: `version`, `doctor`, strict config tests, redaction tests, migration idempotency on an empty database, `go test -race ./...`, `go vet ./...`, and a CGO-disabled build passed locally on Linux/amd64. The checked-in CI workflow has not been run remotely yet.

Known gaps: doctor does not yet verify kernel version, cgroup delegation, user namespaces, overlayfs, seccomp, or devpts. The checked-in CI workflow has not run remotely.

## Phase 1: Docker backend and core API — in progress

Implemented: backend-neutral Runtime/Sandbox interfaces; Docker Engine Unix-socket client; local image inspection and pull; container create/start/destroy with memory, swap, CPU, PID, no-network, dropped-capability and no-new-privileges settings; command exec stream decoding and exit status; file and archive transfer primitives with path/tar checks; snapshot, limit update, stats and kill calls. API key generation, SHA-256-only persistence, revocation, and CLI key commands are implemented. The `cellbox run` CLI command creates a no-network session, streams command output, and destroys the session on completion. The HTTP server exposes health/readiness and authenticated system, session lifecycle, exec wait/SSE, file, and archive routes. Session and exec state/output are persisted; API keys are scope checked; the service creates and prints the first admin key once, refuses non-loopback binds without `--allow-remote`, and supports TLS config.

Validation so far: unit tests cover image metadata, container isolation settings, exec stdout/stderr and exit status, file archive reads, path traversal rejection, and invalid limits. API tests cover health/readiness, key ownership, session creation/list/get/delete, wait-mode exec, resumable output, file round-trip, invalid requests, and OpenAPI response validation against the checked-in contract. `go test -race ./...`, `go vet ./...`, and a CGO-disabled build pass locally. A local smoke run confirmed `/healthz` returns 200, `/v1/system` returns 401 without a key, and returns 200 with a generated admin key. The API walkthrough exists but Docker execution has not been run against a live daemon.

Acceptance status: not met. The shared runtime contract suite is missing; Docker's disk quota depends on storage-driver support, interactive terminals are not implemented, and the full security/limit contract is unproven. API pagination and idempotency remain incomplete. The documented end-to-end walkthrough has not been run against a live Docker daemon (no Docker socket is present in this environment). Do not use this backend for untrusted workloads yet.
