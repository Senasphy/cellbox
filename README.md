# cellbox

cellbox is a self-hostable service for creating disposable computers that AI agents can use. It provides isolated filesystems, resource limits, fail-closed egress proxying, command streaming, snapshots and forks, replay, an embedded web dashboard, SDKs (Python, TypeScript, Go), and an MCP server.

The native Linux, egress, snapshot/fork, warm-pool, and security/chaos acceptance gates pass locally inside a delegated cgroup v2 scope, while some Phase 7 v1.0 release/soak items and a live hosted-model run remain incomplete (see [docs/DEFERRED.md](docs/DEFERRED.md)). Read the security boundaries below before exposing the service or running untrusted workloads.

## Build

Requires Go 1.23 or newer on Linux.

```sh
make build
./bin/cellbox version
./bin/cellbox doctor
```

`cellbox serve` initializes the configured data directory and database, prints a bootstrap admin key once, and starts the preliminary HTTP API. It binds to loopback by default. `cellbox keys create` creates additional admin keys; `cellbox keys ls` lists metadata, and `cellbox keys revoke <id>` revokes a key.

Set `CELLBOX_KEY` to an API key and run a one-off command with `cellbox run --image alpine:latest -- sh -c 'echo hello'`. The command creates an isolated session with networking disabled, streams output, preserves the command's exit status, and destroys the session afterward. Set `CELLBOX_URL` or pass `--url` to use a service at another address.

The currently implemented endpoints and a Docker-backed curl walkthrough are in [docs/API.md](docs/API.md). The Docker runtime contract and API walkthrough passed locally against Docker Engine 29.10.0 on 2026-11-21.

## Security model

The native backend isolates workloads using Linux user, mount, PID, network, IPC, UTS and cgroup namespaces, cgroup v2 resource limits, a seccomp filter and capability dropping. It shares the host kernel, so a kernel vulnerability can allow escape. For hostile multi-tenant workloads, run cellbox inside a dedicated virtual machine. Never expose the service to untrusted networks without TLS and the quotas and egress policy described in the documentation.

The current implementation enforces a soft per-session disk quota by scanning the writable overlay every two seconds; live resource enforcement, security attack-matrix, cleanup/chaos, and native terminal reconnect checks pass inside a delegated cgroup v2 scope (`make test-security`). Host-side file operations use `/proc/<init-pid>/root` with `openat2` confinement, and the terminal reconnect grace period is fixed at 30 seconds (256 KiB scrollback ring). Full 1-hour soak testing and external security review remain incomplete.

The Docker backend applies daemon-managed container limits and hardening options, but it is not the specified native isolation design. The Docker runtime contract and API walkthrough passed locally against Docker Engine 29.10.0; hosted CI has not been rerun for the current work. Its effective protections depend on the Docker-compatible daemon and host configuration. Do not use it for hostile workloads.

For vulnerability reporting, deployment requirements, and the control-by-control risk assessment, see [SECURITY.md](SECURITY.md), [docs/OPERATIONS.md](docs/OPERATIONS.md), and [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md).
