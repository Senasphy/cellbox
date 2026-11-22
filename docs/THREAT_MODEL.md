# Cellbox threat model

This document describes the current single-node implementation and its residual risks. It is not evidence that the full security suite has passed. The native runtime has not completed the specified attack matrix, and its writable-layer disk cap is a two-second polling limit with a possible overshoot. Use a dedicated VM for hostile workloads.

## Required security statement

The native backend isolates workloads using Linux user, mount, PID, network, IPC, UTS and cgroup namespaces, cgroup v2 resource limits, a seccomp filter and capability dropping. It shares the host kernel, so a kernel vulnerability can allow escape. For hostile multi-tenant workloads, run cellbox inside a dedicated virtual machine. Never expose the service to untrusted networks without TLS and the quotas and egress policy described in the documentation.

## Assets

- Host files, host secrets, and credentials available to the Cellbox service account.
- The service's API-key hashes, configuration, image cache, and audit data.
- Each session's files, command output, environment, snapshots, and egress records.
- Other sessions' data and processes.
- Host availability, cgroup capacity, disk space, and the internal network.

## Adversaries

- A malicious model-generated or prompt-injected payload running as a sandbox workload.
- A malicious API client holding a valid session key and attempting to exceed its ownership or quota.
- A network attacker attempting to steal credentials, abuse the API, or reach a protected network through a workload.

An administrator-level API key and a malicious host operator are trusted by this model. A kernel exploit, hardware side channel, and denial of service by an administrator are out of scope.

## Trust boundaries

- **Client to API:** bearer API key, request validation, owner and scope checks, per-key quota and rate enforcement, bounded request and output sizes, and optional TLS for remote binds.
- **API to runtime:** validated session specifications and bounded runtime operations. SQLite owns session, exec, snapshot, audit, and egress records.
- **Host supervisor to sandbox init:** a private Unix `SOCK_SEQPACKET` control channel and a bounded JSON protocol. Descriptors are passed with `SCM_RIGHTS`; the workload helper receives only its explicit request and standard streams.
- **Sandbox to host kernel:** user, mount, PID, network, IPC, UTS, and cgroup namespaces; unprivileged UID/GID mapping; `no_new_privs`; dropped capabilities; and a synchronized seccomp filter. The kernel remains shared.
- **Sandbox to network:** no external interface for the native `none` policy. Allowed HTTP/HTTPS traffic uses a loopback forwarder and a per-session host proxy. Docker sessions currently remain network-disabled.

## Threats, controls, and residual risk

| Threat | Current control | Residual risk or incomplete evidence |
| --- | --- | --- |
| Filesystem escape or host-file access | Native sessions use mount and user namespaces, an overlay root, `pivot_root`, and a detached old root. The only writable host directory bind-mounted into the sandbox is the session run directory, which contains the proxy socket and read-only exec helper; selected device files are also bind-mounted read/write for device semantics. Host-side file API operations use `openat2` with `RESOLVE_IN_ROOT` and `RESOLVE_NO_MAGICLINKS`, validate paths, and cap file/archive transfers. | File operations are performed by the host service through `/proc/<init-pid>/root`, rather than by init inside the sandbox as the original design proposed. The hostile symlink/archive matrix is incomplete; a kernel vulnerability remains an escape path. |
| Privilege escalation | Workloads run in a user namespace with a single host UID mapping, have no capabilities, inherit `no_new_privs`, use `nosuid` sandbox mounts, and receive the default-allow seccomp filter with explicit `EPERM` denials. | The seccomp policy is not a complete syscall allowlist. The full privilege-escalation security suite has not passed on the supported kernel matrix. |
| CPU, memory, PID, disk, output, or descriptor exhaustion | Cgroup v2 controls CPU, memory, and PIDs; admission control checks configured capacity; workload helpers apply rlimits; exec output is capped; and native disk usage is periodically scanned. Cgroup kill and cleanup stop session processes. | The disk cap is soft: the watcher samples every two seconds, so writes can overshoot before termination. CPU/memory/PID enforcement tolerances, descriptor-exhaustion cases, and long soak behavior are not fully verified. |
| Fork bombs and OOM | `pids.max`, memory cgroup limits, OOM/PID counter observation, and cgroup-wide kill contain workload growth and stop affected sessions. | Orphaned descendants are not proactively reaped by init; cgroup cleanup is relied upon. The full fork-bomb and OOM attack cases have not passed. |
| Network abuse and SSRF | Native `none` policy has no external interface. The proxy normalizes IDNA names, checks all resolved addresses against private/reserved/blocked ranges, dials the validated IP, verifies the connected peer, reauthorizes HTTP redirects, and logs allow/deny decisions. | Live sandbox tests have not proved that workloads cannot bypass proxy variables. The Docker backend supports only `none`; allowlisted package-manager workflows and full DNS-rebinding tests remain incomplete. |
| Cross-session interference | Separate namespaces, cgroups, runtime directories, owner-scoped API access, and immutable snapshot layers isolate session state. | Isolation depends on the shared host kernel and filesystem. Native fork/snapshot isolation has not passed the full live acceptance matrix. |
| Attack on supervisor or control channel | The control socket is not mounted into the workload. Init is non-dumpable; ptrace and related process-memory syscalls are denied; helper descriptors are explicitly bounded; protocol messages and workload inputs are size-checked. | The protocol and supervisor have focused tests but no complete fuzzing or chaos results. A supervisor or kernel defect can affect the host service. |
| API abuse or credential exposure | API keys are stored as hashes, compared in constant time, returned only once, and scoped. Session keys receive active-session, memory, and request-rate quotas. POST idempotency, ownership checks, and bounded bodies/output reduce accidental or abusive load. | Admin keys are unmetered and can cause denial of service. Remote binds require an explicit flag, but operators must configure TLS and network controls correctly. |

## Operational limits

The native backend is a Linux containment layer, not a VM boundary. The Docker backend uses daemon-managed container isolation and currently has no per-session writable-layer disk cap. Neither backend should be treated as safe for hostile multi-tenant workloads on a shared host without the dedicated-VM boundary described above. Review [OPERATIONS.md](OPERATIONS.md), [SECCOMP.md](SECCOMP.md), and [DEFERRED.md](DEFERRED.md) before deployment.
