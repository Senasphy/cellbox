# Threat model

This document describes cellbox's single-node security boundaries, controls, and residual risks.

## Security boundary

The native backend isolates workloads using Linux user, mount, PID, network, IPC, UTS, and cgroup namespaces, cgroup v2 resource limits, a seccomp filter, and capability dropping. Because it shares the host kernel, a kernel vulnerability can allow escape. **For hostile multi-tenant workloads, run cellbox inside a dedicated virtual machine.** Never expose the service to untrusted networks without TLS, quotas, and an explicit egress policy.

## Assets

- Host files, secrets, and credentials accessible to the cellbox service user.
- API-key hashes, service configuration, cached images, and audit logs.
- Per-session filesystems, command output, environment variables, snapshots, and egress logs.
- Other sessions' data and processes.
- Host availability, cgroup capacity, disk space, and internal networks.

## Adversaries

- **Untrusted workload**: a malicious or prompt-injected payload executing inside a sandbox.
- **Malicious session client**: a caller with a valid session-scoped API key trying to exceed its quotas or access another owner's sessions.
- **Network attacker**: an external party attempting to steal credentials, abuse the API, or pivot into internal networks via a sandbox.

Admin-level API keys and the host operator are trusted. Kernel exploits, hardware side channels, and denial-of-service by an administrator are out of scope.

## Trust boundaries

- **Client → API**: Bearer API key authentication, request validation, owner/scope checks, per-key rate and resource quotas, bounded request/output sizes, and optional TLS.
- **API → Runtime**: Validated session specs and bounded runtime calls; SQLite owns session, exec, snapshot, audit, and egress records.
- **Host supervisor → Sandbox init**: Private Unix `SOCK_SEQPACKET` channel with a bounded JSON protocol and `SCM_RIGHTS` descriptor passing.
- **Sandbox → Host kernel**: User, mount, PID, network, IPC, UTS, and cgroup namespaces; unprivileged UID/GID mapping; `no_new_privs`; dropped capabilities; and a synchronized seccomp filter on a shared kernel.
- **Sandbox → Network**: No external interface for `none` policy. Allowed HTTP/HTTPS traffic goes through a loopback forwarder and per-session host egress proxy. Docker sessions remain network-disabled.

## Threats, controls & residual risk

| Threat | Control | Residual risk / caveats |
| --- | --- | --- |
| Filesystem escape or host file access | Mount and user namespaces, overlayfs root, `pivot_root`, and detached old root. Only the session run directory (proxy socket and read-only exec helper) and selected device nodes are bind-mounted. File API ops run inside `cellbox-init` over `SOCK_SEQPACKET` + `SCM_RIGHTS` using `openat2` (`RESOLVE_IN_ROOT` / `RESOLVE_NO_MAGICLINKS`, `RESOLVE_BENEATH` / `RESOLVE_NO_SYMLINKS`) with validated paths and capped transfers. | A host kernel or overlayfs vulnerability remains an escape path. |
| Privilege escalation | Single-UID user namespace mapping, zero capabilities, `no_new_privs`, `nosuid` mounts, and a synchronized seccomp filter (`EPERM` on denied syscalls). | Seccomp uses a default-allow profile with an explicit denylist (see [SECCOMP.md](SECCOMP.md)), not a full syscall allowlist. |
| Resource exhaustion (CPU, memory, PIDs, disk, output) | Cgroup v2 CPU, memory, and PID limits; admission capacity checks; helper `rlimit`s; capped exec output; and periodic overlay disk scanning. | Native disk cap is soft: the watcher polls every 2 seconds, so fast writes can briefly overshoot before termination. Docker mode does not enforce per-session disk caps. |
| Fork bombs & OOM | `pids.max`, memory cgroup limits, OOM/PID counter tracking, cgroup-wide kill on cleanup, and PID 1 zombie reaping (`cellbox-init`). | Extreme fork/memory pressure still briefly consumes host kernel resources before cgroup limits trip. |
| Network abuse & SSRF | Native `none` has no external interface. The egress proxy normalizes IDNA hostnames, resolves DNS on the host, blocks private/reserved/extra-blocked CIDRs, verifies connected peer IPs, re-validates HTTP redirects, and logs every allow/deny decision. | Workloads must honor `HTTP_PROXY` / `HTTPS_PROXY` (direct sockets fail closed with no route). Docker backend only supports `none`. |
| Cross-session interference | Separate namespaces, cgroups, runtime directories, owner-scoped API access, and immutable snapshot layers. | Isolation relies on the shared host kernel and filesystem. |
| Supervisor / control-channel attack | Control socket is not mounted into the workload; init is non-dumpable; `ptrace` and cross-process memory syscalls are blocked by seccomp; helper FDs and protocol messages are bounded. | A bug in the supervisor protocol or host kernel could affect the service process. |
| API abuse or credential exposure | Keys are hashed at rest, compared in constant time, shown once on creation, and scoped (`admin` vs `session`). Session keys have active-session, memory, and request-rate quotas; non-loopback binds require `--allow-remote`. | Admin keys are unmetered. Operators are responsible for enabling TLS and firewalling remote binds. |

## Operational limits

The native backend is Linux process containment, not a VM boundary. The Docker backend delegates containment to the Docker daemon and does not cap per-session writable-layer disk usage. Review [OPERATIONS.md](OPERATIONS.md) and [SECCOMP.md](SECCOMP.md) before deploying.
