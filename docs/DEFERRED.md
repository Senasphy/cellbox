# Deferred specification details

This report records what is still missing from the requested build. The goal remains the full project; these omissions do not count as acceptance.

## Core blockers

- Phase 3 is not accepted: the real-model fix-loop has not been run with a live model, and the Playwright walkthrough has had recent failures. The last known all-green image-page browser run is [39432855214](https://github.com/Senasphy/cellbox/actions/runs/39432855214).
- Phase 4 is not accepted: the native backend still has no `Runtime` implementation or workload supervisor. The current work only wires the namespace init entry point and installs the seccomp policy after its mount setup. Image pull/flatten, command launch, PTY, sandbox file operations, OOM/breach monitoring, and cleanup orchestration remain missing or incomplete.
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

`SpawnInit` re-executes the service binary as `cellbox-init`. `cmd/cellbox` now dispatches that argv[0] to `RunInit`; the init installs the existing default seccomp filter after namespace/mount setup and before reporting ready. Focused package checks and a static Linux amd64 build passed locally. This enables the init path but does not provide workload execution or constitute native-runtime acceptance.
