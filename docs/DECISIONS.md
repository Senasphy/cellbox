# Decisions

## Phase 0

- Context: The specification requires prefixed sortable ULIDs, and the environment has no guaranteed network access for Go modules. Decision: Encode a 48-bit UTC millisecond timestamp and 80 bits from `crypto/rand` with the ULID Crockford alphabet using the standard library. Consequence: IDs remain sortable and ULID-shaped without another dependency; monotonic ordering for IDs generated in the same millisecond is not guaranteed.
- Context: Database writes need a single writer and zero setup. Decision: Use modernc SQLite with one open connection and WAL mode. Consequence: Pure Go builds work without CGO; write concurrency is serialized.
- Context: The first phase needs a runnable doctor before native runtime checks exist. Decision: Report baseline OS and cgroup-v2 presence and label delegation checks as not implemented. Consequence: Doctor is explicitly incomplete until Phase 4.
- Context: The Docker backend requires a configurable Engine or Podman-compatible Unix socket. Decision: Add `docker_socket` with `/var/run/docker.sock` as the default and a `CELLBOX_DOCKER_SOCKET` override. Consequence: The endpoint follows the rest of the config layering and remains optional for the native backend.

## Phase 1

- Context: The Docker Engine API has no endpoint to signal one `exec` process independently. Decision: On an exec timeout, stop the containing Docker container so the workload process tree cannot keep running. Consequence: The whole session is marked failed after a timeout; this is a bootstrap-backend limitation and is less precise than the native process supervisor.
- Context: Docker per-container storage quotas are not available with common storage configurations; for overlay2 Docker documents that they require an XFS backing filesystem mounted with project quotas ([Docker Engine daemon reference](https://docs.docker.com/reference/cli/dockerd/)). Decision: The Docker runtime does not claim or set a disk limit, advertises `disk_limits: false`, reports `disk_mb: 0` when no disk quota is requested, and rejects explicit disk limit requests. Consequence: Docker sessions have no enforced writable-layer size cap and remain unsuitable for hostile workloads; a future backend can advertise this capability only when enforcement is reliable.
- Context: Network allowlists and proxying are not implemented in the first backend slice. Decision: Accept only an empty policy or `{"mode":"none"}` and always create Docker containers with `NetworkMode=none`. Consequence: Any requested network access fails closed until the egress phase is implemented.
