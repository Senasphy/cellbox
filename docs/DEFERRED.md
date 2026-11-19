# Deferred specification details

This report records what is still missing from the requested build. The goal remains the full project; these omissions do not count as acceptance.

## Core blockers

- Phase 3 is not accepted: the real-model fix-loop has not been run with a live model, and the Playwright walkthrough has had recent failures. The last known all-green image-page browser run is [39432855214](https://github.com/Senasphy/cellbox/actions/runs/39432855214).
- Phase 4 is not accepted: the native backend now has an initial `Runtime` with OCI image pull/flatten, namespace/cgroup session creation, bounded command execution, basic stats, and cleanup. It has not passed the shared runtime contract. PTY, file/archive operations, per-process signals and live output streaming, OOM/breach monitoring, disk enforcement, and full cleanup/chaos coverage remain missing or incomplete.
- The Docker backend has no enforced per-session writable-layer disk limit and is not suitable for hostile workloads.
- Phase 5 egress proxy/policy and the dashboard Network view are not implemented.
- Phase 6 warm pool, snapshots/fork, and replay are not implemented end to end.
- Phase 7 hardening, packaging, threat-model validation, and v1.0 definition-of-done evidence remain incomplete.
- Phase 8 cluster mode is optional and deferred until single-node acceptance is complete.

## Smaller details deferred for now

- Complete visual theming, manual WCAG review, terminal 60 fps measurement, and the remaining specified Files behaviors have not been audited to the specification's full quality bar.
- Some dashboard operations still use direct `fetch` rather than generated typed operations, and live SSE behavior does not yet meet every reconnect/replay detail in the specification.
- The SDKs, tool schemas, MCP server and CLI have implemented core paths, but exhaustive compatibility, shell completions, and commands that depend on snapshots/fork APIs are incomplete.
- Resource breach recording currently covers only some sources; timeout, OOM, pids, and disk reporting are not complete.

## Recently repaired core wiring

`SpawnInit` re-executes the service binary as `cellbox-init`. The existing `cmd/cellbox/native_init.go` hook dispatches that argv[0] to `RunInit`; init installs the default seccomp filter after its mount setup and before reporting ready. The init now serves bounded exec requests. A native Runtime creates overlay-backed sessions under per-session cgroups, pulls and flattens host-architecture OCI images, and supports command execution, stats, limit updates, kill, and destroy. Focused image/runtime package checks, vet, and a static Linux amd64 build pass locally. Native acceptance remains unproven because the shared contract and security/resource suites cannot run without a fully delegated host cgroup and the other features listed above.
