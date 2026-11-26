# Changelog

All notable Cellbox changes are recorded here. The project has not published a stable v1.0 release yet.

## v0.1.0 (2026-11-26)

### Added

- A single-node API, CLI, dashboard, and SDK surface for disposable sandbox sessions.
- Docker and native Linux runtime backends, session quotas and lifecycle recovery, audit events, and Prometheus metrics.
- Native per-session egress proxying, snapshots and forks, replay endpoints, asciicast export, and a bounded warm pool.
- Embedded web dashboard rebuilt on Farm.js (`@farm.js/core`) with dark/light themes, live PTY terminal, timeline, file browser/CodeMirror viewer, uPlot resource charts, egress log, snapshots/fork, replay, and `⌘K` command palette.
- Python, TypeScript, and Go clients, an MCP server, model tool schemas, and example agent/evaluation workflows.
- System and user systemd units, an operations guide, a threat model, a Linux release installer, GoReleaser configuration, and a demo Docker image/Compose setup.
- Native PID 1 reaps adopted zombie descendants while preserving helper exit statuses managed by Go.
- Native security attack-matrix, cleanup/chaos, and live terminal reconnect tests (`make test-security`), five Go fuzz targets (`make fuzz`), and a concurrent load/soak generator (`test/load`).

### Security and verification notes

- The native runtime shares the host kernel. Use a dedicated VM for hostile multi-tenant workloads.
- The Docker backend does not enforce a per-session writable-layer disk cap.
- External security review, `staticcheck`/`golangci-lint`/`govulncheck`, a full 1-hour soak run, and live hosted-model verification remain incomplete.
