# cellbox

cellbox is a self-hostable service for creating disposable computers that AI agents can use. It is intended to provide isolated filesystems, resource limits, controlled networking, command streaming, snapshots, and replay through an API and dashboard.

This repository is in the early phases of the build specification. It provides configuration validation, SQLite persistence, local API key management, basic health and system endpoints, and an initial Docker runtime implementation. Session APIs and complete workload isolation are still under development; do not use it to run untrusted code yet.

## Build

Requires Go 1.23 or newer on Linux.

```sh
make build
./bin/cellbox version
./bin/cellbox doctor
```

`cellbox serve` initializes the configured data directory and database, prints a bootstrap admin key once, and starts the preliminary HTTP API. It binds to loopback by default. `cellbox keys create` creates additional admin keys; `cellbox keys ls` lists metadata, and `cellbox keys revoke <id>` revokes a key.

The currently implemented endpoints and a Docker-backed curl walkthrough are in [docs/API.md](docs/API.md). The walkthrough is documented but has not been verified against a live Docker daemon.

## Security model

The native backend isolates workloads using Linux user, mount, PID, network, IPC, UTS and cgroup namespaces, cgroup v2 resource limits, a seccomp filter and capability dropping. It shares the host kernel, so a kernel vulnerability can allow escape. For hostile multi-tenant workloads, run cellbox inside a dedicated virtual machine. Never expose the service to untrusted networks without TLS and the quotas and egress policy described in the documentation.

This statement describes the target native backend. No sandbox backend is implemented yet, so the current repository provides no workload isolation.
