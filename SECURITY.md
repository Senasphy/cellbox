# Security policy

The native backend isolates workloads using Linux user, mount, PID, network, IPC, UTS and cgroup namespaces, cgroup v2 resource limits, a seccomp filter and capability dropping. It shares the host kernel, so a kernel vulnerability can allow escape. For hostile multi-tenant workloads, run cellbox inside a dedicated virtual machine. Never expose the service to untrusted networks without TLS and the quotas and egress policy described in the documentation.

## Reporting a vulnerability

Please report suspected vulnerabilities through GitHub's private vulnerability reporting for `Senasphy/cellbox`. Do not include API keys, session contents, or other user data in a report. If private reporting is unavailable, contact the repository owner privately and do not open a public issue with exploit details. Include the affected commit or release, backend, Linux distribution and kernel version, impact, and a minimal reproduction when safe to provide.

The maintainers will acknowledge reports privately, investigate the impact, and coordinate a fix and disclosure with the reporter. Do not publish exploit details before a fix or coordinated disclosure.

## Supported versions

Cellbox has not yet published a stable v1.0 release. Before v1.0, security fixes are made on the current default branch; there is no support commitment for older commits. After v1.0, the latest v1.x release is the supported line. Upgrade to the latest patch release to receive security fixes.

## Deployment guidance

Use the native backend on a dedicated Linux host or VM. Because it shares the host kernel, do not use it as the only boundary for hostile multi-tenant workloads. The Docker backend is a compatibility backend and does not enforce a per-session writable-layer disk limit. See [the threat model](docs/THREAT_MODEL.md), [operations guide](docs/OPERATIONS.md), and [deferred acceptance gaps](docs/DEFERRED.md).
