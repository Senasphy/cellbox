# Changelog

All notable Cellbox changes are recorded here. The project has not published a stable v1.0 release yet.

## Unreleased

### Added

- A single-node API, CLI, dashboard, and SDK surface for disposable sandbox sessions.
- Docker and native Linux runtime backends, session quotas and lifecycle recovery, audit events, and Prometheus metrics.
- Native per-session egress proxying, snapshots and forks, replay endpoints, asciicast export, and a bounded warm pool.
- Python, TypeScript, and Go clients, an MCP server, model tool schemas, and example agent/evaluation workflows.
- System and user systemd units, an operations guide, a threat model, a Linux release installer, GoReleaser configuration, and a demo Docker image/Compose setup.
- Dashboard session creation supports image and network policy selection; the Network view pages through older egress decisions.
- Native PID 1 reaps adopted zombie descendants while preserving helper exit statuses managed by Go.

### Security and verification notes

- The native runtime shares the host kernel. Use a dedicated VM for hostile multi-tenant workloads.
- The Docker backend does not enforce a per-session writable-layer disk cap.
- Full native security, resource, cleanup/chaos, network-bypass, snapshot-isolation, warm-pool latency, soak, and release-pipeline acceptance remains incomplete. See [docs/DEFERRED.md](docs/DEFERRED.md) for current gaps.
