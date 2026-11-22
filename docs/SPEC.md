sandboxd: Build Specification




  cellbox: Build Specification
  Working name: cellbox. Audience: an autonomous coding agent that will build the project
  end to end. Status: v1.0, authoritative. Save a copy in the repo as docs/SPEC.md.


  0. How to use this document
  You are the engineer. Read the whole document before writing code. Then:

    1. Build in the phases of section 20. Do not start a phase until the acceptance criteria of
       the previous phase pass.
    2. When this spec is silent or ambiguous, pick the simpler option and record it as a short
       entry in docs/DECISIONS.md (context, decision, consequence).
    3. Every behavior ships with tests. A feature without tests is not done.
    4. Be honest about security. Never write documentation, comments or README text that
       claims stronger isolation than the code provides. Section 17 defines the required
       wording.
    5. Prefer the standard library and a small number of well-known dependencies. Justify
       each dependency in docs/DEPENDENCIES.md.
    6. Commit small and often with conventional commit messages. Keep the main branch
       building and green.
    7. Where this spec says 'spike', time-box an experiment, write the finding into
       docs/DECISIONS.md, then implement.
    8. Do not add features that are not in this document. Non-goals are listed in section 2.


  1. Project summary
  cellbox is a self-hostable service that gives AI agents safe, disposable computers.

  A client (an agent, a script or a human) creates a session: a sandbox with its own
  filesystem, CPU, memory and process limits, and a network policy. The client runs
  commands in it with live streamed output, reads and writes files, snapshots and forks the
  filesystem, and destroys it when done. A web dashboard provides live terminals, a file
  browser, resource graphs, a command timeline, an egress log and session replay.

  The agent loop and the language model stay outside cellbox. cellbox is a tool that the
  agent calls. It never holds model API keys and never talks to a model.



                                                                                           Page 1 of 35
sandboxd: Build Specification




  Why it exists
        AI agents increasingly need to execute code that a model just wrote. That code is
        untrusted by definition, and running it on a developer laptop or a shared server is
        dangerous (deleted files, leaked secrets, runaway CPU, prompt-injection attacks,
        cross-user interference).
        Hosted sandbox products send code and data to a third party. Some teams cannot or
        will not do that, and some want lower cost or lower latency by running on their own
        hardware.
        Evaluating agents requires many parallel, reproducible, fully logged runs in clean
        environments. The same machinery serves that need.


  Primary users
    1. Developers building agents with a code-interpreter or coding capability.
    2. Builders of agent evaluations and benchmarks who need isolated, reproducible,
       parallel runs.
    3. Learning and assessment platforms that run submitted code.


  Differentiators (build to these, they are the story)
        Rootless: no root daemon and no Docker daemon required.
        Native runtime built directly on Linux namespaces, cgroups v2, overlayfs and
        seccomp, with a written threat model.
        Fail-closed networking: no network unless allowed, an egress proxy with allowlist,
        SSRF protection and full logging.
        Snapshot and fork of sandbox filesystems, for agent branching and reproducible
        evaluations.
        Full audit and replay of every session.
        One static binary with an embedded dashboard, plus an MCP server so any MCP-
        capable agent can use it with no code.


  2. Goals, non-goals and targets

  Goals
        Production-quality single-node service: correct, observable, well tested, easy to install.




                                                                                              Page 2 of 35
sandboxd: Build Specification




        A pluggable isolation interface with two backends: a Docker-API backend (bootstrap)
        and the native rootless backend (the core of the project).
        A polished dashboard and first-class SDKs and tooling for agents.
        Optional cluster mode (multiple workers) as the final phase. Single-node quality comes
        first.


  Non-goals (do not build)
        Hosting for third parties, billing, multi-organization tenancy, or a public demo server.
        Running agent loops or calling LLM APIs inside cellbox.
        macOS or Windows native support.
        GPU passthrough, nested virtualization, or persistent sessions across service restarts.
        Sessions are ephemeral; snapshots persist.
        Claiming protection against kernel zero-days or hardware side channels.


  Performance and scale targets
  Measure on a 4-core, 8 GB machine and publish the real numbers in
  docs/BENCHMARKS.md. Never fabricate numbers. If a target is missed, report the real
  value and explain.


     Metric                                                 Target

     Create session, image cached, no warm pool, p50        under 400 ms

     Create session from warm pool, p50                     under 60 ms

     Exec overhead for a trivial command, p50               under 15 ms

     Idle sandbox memory overhead                           under 15 MB each

     Concurrent idle sessions                               at least 200

     Concurrent CPU-bound sessions without gateway          at least 50
     latency degradation

     Output streaming throughput (single exec, SSE)         at least 50 MB/s

     Time from limit breach to process tree dead            under 200 ms




                                                                                              Page 3 of 35
sandboxd: Build Specification




  3. Platform and technology decisions
  Platform: Linux only. Hard minimum kernel 5.14 (rootless overlayfs needs 5.11,
  CLONE_INTO_CGROUP needs 5.7, cgroup.kill needs 5.14). Recommended 6.1 or newer.
  cgroup v2 unified hierarchy with a delegated subtree is required for the native backend.
  Development happens on Arch Linux. CI runs on Ubuntu 24.04. macOS and Windows
  users run cellbox inside a Linux VM or container.


     Area                       Decision                                     Rationale

     Language                   Go 1.23 or newer                             Static binaries, strong
                                                                             concurrency, good syscall
                                                                             access

     HTTP                       net/http with Go 1.22 method-pattern         No framework needed
                                ServeMux

     WebSocket                  github.com/coder/websocket                   Context-aware, small,
                                                                             maintained

     CLI                        github.com/spf13/cobra                       Standard for subcommand
                                                                             binaries

     Config                     YAML via gopkg.in/yaml.v3, env var           Predictable layering
                                overrides, flags highest priority

     Logging                    log/slog, JSON in production, text in dev    Standard library

     Metrics                    github.com/prometheus/client_golang          De facto standard

     Tracing                    OpenTelemetry SDK, optional, off by          Useful for cluster mode
                                default

     PTY                        github.com/creack/pty                        Mature

     Seccomp                    github.com/elastic/go-seccomp-bpf (pure      Avoids CGO, keeps a static
                                Go, TSYNC support)                           binary

     OCI images                 github.com/google/go-containerregistry       Pull and unpack images with
                                                                             no daemon

     Docker                     github.com/docker/docker client              Bootstrap backend only
     backend

     Store (single              SQLite via modernc.org/sqlite (pure Go) in   No CGO, zero setup
     node)                      WAL mode



                                                                                                       Page 4 of 35
sandboxd: Build Specification




     Area                       Decision                                      Rationale

     Store (cluster)            Postgres via jackc/pgx, behind the same       Phase 8 only
                                Store interface

     Migrations                 Embedded SQL files, applied by a small        Reproducible schema
                                internal runner or pressly/goose

     IDs                        ULIDs with a type prefix (ses_, exe_, snp_,   Sortable and readable
                                key_)

     Dashboard                  Next.js 15 App Router, TypeScript strict,     Embedded into the binary
                                Tailwind, static export (output: export)      with go:embed

     Dashboard                  xterm.js with fit and webgl addons, uPlot     Fast and light
     libraries                  for time series, TanStack Query for data

     Dashboard                  Playwright                                    End to end against a real
     tests                                                                    binary

     Packaging                  goreleaser, GitHub Actions, Docker image,     Multiple install paths
                                systemd unit, AUR PKGBUILD


  CGO is disabled in all release builds (CGO_ENABLED=0). If a dependency requires CGO,
  replace it.


  4. Architecture
     Clients: SDKs, CLI, MCP server, dashboard, curl
               | REST + SSE + WebSocket (HTTP/1.1 or HTTP/2)
               v
     +---------------------------------------------------------------+
     | cellbox serve (single process in single-node mode)           |
     |                                                               |
     | API layer       auth, quotas, rate limits, validation         |
     | Session mgr     one actor goroutine per session, state machine|
     | Scheduler       picks a worker (trivial in single-node mode) |
     | Warm pool       pre-created sandboxes per image               |
     | Runtime iface docker backend | native backend                |
     | Egress proxy    one listener per session, policy + logging    |
     | Metrics         cgroup sampler, Prometheus endpoint           |
     | Store           SQLite (sessions, execs, snapshots, keys)     |
     | Log store       append-only per-exec output logs (replay)     |




                                                                                                          Page 5 of 35
sandboxd: Build Specification




     | Dashboard       embedded static assets served at /            |
     +---------------------------------------------------------------+
               | clone(2) with namespaces, cgroup v2, overlayfs
               v
       Sandboxes: init (PID 1 of its pid namespace) + workload processes



  Process model
   cellbox serve runs as an unprivileged user. It creates sandboxes with user namespaces,
  so no root is needed. Every sandbox is a child process tree created by re-executing the
  cellbox binary itself (the re-exec pattern). The child starts directly inside new
  namespaces and inside its own cgroup (CLONE_INTO_CGROUP via
  SysProcAttr.UseCgroupFD in Go). If the service dies, sandboxes die with it
  (PR_SET_PDEATHSIG). On startup the service reconciles: it kills leftover cgroups and
  marks orphaned sessions as lost.


  Lifecycle: create session
    1. API validates the request, authenticates the key, checks quotas.
    2. Session row is inserted in state creating. A session actor goroutine is started.
    3. The scheduler selects the worker (the local one in single-node mode).
    4. The runtime claims a warm sandbox if one exists for the image, otherwise creates a
       new one. Limits and network policy are applied.
    5. State moves to ready. The response returns the session object.


  Lifecycle: run a command
    1. API validates the exec request and checks that the session is ready.
    2. The session actor sends an exec request to the sandbox init over the control channel.
    3. Init spawns the process with a PTY or pipes, and returns the file descriptors to the
       worker using SCM_RIGHTS.
    4. The worker pumps output into the per-exec log and to any attached streams. Exit
       status arrives on the control channel.
    5. The exec row is finalized with exit code, duration, peak memory and output size.




                                                                                          Page 6 of 35
sandboxd: Build Specification




  Repository layout

     cmd/cellbox/               main, cobra commands (serve, doctor, keys, run, mcp,
     version)
     internal/api/              HTTP handlers, middleware, problem+json errors, SSE
     and WS helpers
     internal/auth/             key generation, hashing, scopes, quotas
     internal/session/          session manager, actors, state machine, TTL and
     reaping
     internal/runtime/          Runtime and Sandbox interfaces, shared types,
     contract test suite
     internal/runtime/docker/ Docker-API backend
     internal/runtime/native/ native backend (spawn, init, cgroups, seccomp,
     mounts, caps)
     internal/image/            OCI pull, flatten, content-addressed rootfs cache
     internal/egress/           per-session proxy, policy engine, IP blocklist,
     logging
     internal/pool/             warm pool
     internal/store/            Store interface, sqlite and postgres
     implementations, migrations
     internal/logstore/       append-only output logs, replay reader
     internal/metrics/          cgroup sampler, Prometheus collectors
     internal/doctor/           prerequisite checks
     internal/config/           config loading and validation
     web/                       Next.js dashboard (built and embedded into the
     binary)
     sdk/python/                Python SDK
     sdk/typescript/            TypeScript SDK
     examples/                  agent examples (hosted model, Ollama, eval runner)
     docs/                    SPEC, DECISIONS, DEPENDENCIES, SECURITY,
     THREAT_MODEL, BENCHMARKS, API
     api/openapi.yaml           the API contract
     deploy/                    systemd unit, compose file, PKGBUILD
     test/                      integration, security and load tests




                                                                                    Page 7 of 35
sandboxd: Build Specification




  5. Domain model

  Session
  Fields: id, owner key id, image reference and resolved digest, state, limits (cpu millicores,
  memory MB, pids, disk MB, wall-clock TTL seconds, idle TTL seconds), network policy, env,
  labels, created, last activity, expires, parent snapshot id (when forked), failure reason.

  States: creating, ready, stopping, stopped, expired, failed, lost. Transitions are serialized
  by the session actor and each transition is persisted before side effects are reported.
  Terminal states are stopped, expired, failed and lost.


  Exec
  Fields: id, session id, argv or shell string, cwd, env overrides, tty flag, timeout, state
  (starting, running, exited, killed, timed_out, failed), exit code, signal, started, ended, peak
  memory, output bytes, truncated flag, output log reference.


  Snapshot
  Fields: id, source session id, parent snapshot id, layer path, size, created, labels. Snapshots
  form a tree. They are immutable.


  API key
  Fields: id, name, prefix, SHA-256 hash of the secret, scope (admin or session), quotas
  (max sessions, max total memory MB, requests per second), created, last used, revoked.


  Other tables
        images: reference, digest, rootfs path, size, pulled at.
        audit_events: id, session id, key id, type, timestamp, JSON detail. Event types include
        session.create, exec.start, exec.end, file.write, file.read, snapshot.create, limit.breach,
        egress.allow, egress.deny.
        egress_log: session id, timestamp, host, port, resolved ip, verdict, reason, bytes up,
        bytes down, duration.
        idempotency: key hash, request hash, response body, created.

  All timestamps are UTC. Schema changes go through numbered, forward-only migrations.




                                                                                               Page 8 of 35
sandboxd: Build Specification




  6. Public API
  The contract lives in api/openapi.yaml, written first and kept in sync by a contract test in
  CI (use github.com/getkin/kin-openapi to validate real responses).


  Conventions
        Base path /v1. JSON bodies. Snake_case field names.
        Errors use RFC 9457 problem+json with a stable machine-readable code field (for
        example session_not_ready, quota_exceeded, limit_invalid, path_outside_sandbox).
        Every mutating POST accepts an Idempotency-Key header. The same key with the
        same body within 24 hours returns the stored response. The same key with a different
        body returns 422.
        Pagination uses cursors. Lists accept a limit and a cursor.
        Auth: Authorization: Bearer <key> . Keys look like cbx_<8 char prefix>_<32 byte
        secret in base32>. Only the SHA-256 of the secret is stored. Comparison is constant
        time. A session-scoped key sees only its own sessions. An admin key sees everything
        and can manage keys and images.
        Request size limits and timeouts are explicit on every route.


  Endpoints

     Method and path                                    Purpose

     POST /v1/sessions                                  Create a session (image, limits, network
                                                        policy, env, labels, ttl, optional
                                                        from_snapshot)

     GET /v1/sessions, GET /v1/sessions/{id}            List and get

     DELETE /v1/sessions/{id}                           Destroy

     POST /v1/sessions/{id}/execs                       Start an exec. Body: argv or shell, cwd,
                                                        env, timeout_ms, tty. Query wait=true
                                                        blocks and returns the result with output
                                                        capped at 1 MiB

     GET /v1/sessions/{id}/execs, GET .../execs/{eid}   List and get

     GET /v1/sessions/{id}/execs/{eid}/stream           SSE stream of output, resumable with
                                                        Last-Event-ID



                                                                                               Page 9 of 35
sandboxd: Build Specification




     Method and path                                Purpose

     POST /v1/sessions/{id}/execs/{eid}/signal      Send a signal

     GET /v1/sessions/{id}/terminal                 WebSocket interactive PTY

     GET and PUT and DELETE                         Read, write, delete a file
     /v1/sessions/{id}/files?path=

     GET /v1/sessions/{id}/files/list?path=         Directory listing

     GET and PUT /v1/sessions/{id}/archive?path=    Download or upload a tar stream of a
                                                    subtree

     POST and GET /v1/sessions/{id}/snapshots       Create and list snapshots

     POST /v1/sessions/{id}/fork                    Snapshot then create a new session from
                                                    it in one call

     GET /v1/snapshots/{id}, DELETE                 Get and delete
     /v1/snapshots/{id}

     GET /v1/sessions/{id}/metrics, GET             Current stats and SSE stream
     .../metrics/stream

     GET /v1/sessions/{id}/egress-log               Network log

     GET /v1/sessions/{id}/events                   Audit events

     GET /v1/sessions/{id}/replay                   Replay data (timestamped output chunks
                                                    across all execs)

     POST and GET and DELETE /v1/images             Pull, list, remove cached images (admin)

     POST and GET and DELETE /v1/keys               Manage API keys (admin)

     GET /v1/system                                 Capacity, backend name, kernel
                                                    capabilities, doctor results

     GET /healthz, /readyz, /metrics                Liveness, readiness, Prometheus



  Streaming formats
  SSE exec stream. Events are output (fields: seq, ts, stream as stdout or stderr or tty,
  data as base64), exit (exit code, signal, duration, truncated), and error . Each event
  carries an id equal to the byte offset in the exec output log, so a reconnecting client sends




                                                                                           Page 10 of 35
sandboxd: Build Specification




  Last-Event-ID and resumes with no loss, because output is persisted before it is
  delivered. A comment heartbeat is sent every 15 seconds.

  WebSocket terminal. Binary frames. The first byte is the frame type: 0 data (client to
  server is stdin, server to client is terminal output), 1 resize (followed by two big-endian
  uint16: columns, rows), 2 ping, 3 pong, 4 exit (followed by an int32 exit code). On
  disconnect the PTY stays alive for a configurable grace period so the client can reattach
  and receive recent scrollback (keep a 256 KiB ring buffer per terminal).


  Limits and quotas
  Server-wide defaults and maximums live in config. A create request may lower but not
  exceed the maximums. Defaults: 1 CPU, 512 MB memory, 256 pids, 1024 MB disk, 10 minute
  idle TTL, 1 hour wall-clock TTL, exec timeout 60 seconds, exec output cap 16 MiB, file size
  cap 256 MiB per transfer. Exceeding a cap yields a clear, coded error, never a hang.


  7. Runtime abstraction
  All isolation sits behind one interface so backends are interchangeable and share a
  contract test suite.

     type Runtime interface {
         Name() string
         Capabilities() Capabilities // snapshots, tty, cpu limits, egress policy,
     warm pool, etc.
             EnsureImage(ctx context.Context, ref string) (ImageInfo, error)
             Create(ctx context.Context, spec SandboxSpec) (Sandbox, error)
             Reconcile(ctx context.Context) error // kill orphans after a restart
     }


     type Sandbox interface {
             ID() string
             Exec(ctx context.Context, req ExecRequest) (ExecHandle, error)
             OpenTerminal(ctx context.Context, req TerminalRequest) (Terminal, error)
             ReadFile(ctx context.Context, path string) (io.ReadCloser, FileInfo,
     error)
         WriteFile(ctx context.Context, path string, r io.Reader, mode
     fs.FileMode) error
         ListDir(ctx context.Context, path string) ([]FileInfo, error)
             Archive(ctx context.Context, path string) (io.ReadCloser, error)
             Extract(ctx context.Context, path string, r io.Reader) error




                                                                                         Page 11 of 35
sandboxd: Build Specification




             Snapshot(ctx context.Context) (SnapshotInfo, error)
             UpdateLimits(ctx context.Context, l Limits) error
             Stats(ctx context.Context) (Stats, error)
             Kill(ctx context.Context) error
             Destroy(ctx context.Context) error
     }


  The contract test suite in internal/runtime runs against every backend and covers: create
  and destroy, exec exit codes and signals, stdout and stderr separation, TTY behavior,
  timeouts, output caps, file round trips, path escape attempts, limit enforcement, kill
  semantics, and snapshot and fork where the backend declares the capability. A backend
  that does not support a capability returns ErrUnsupported and the API reports a coded
  error.



  8. Docker backend (bootstrap)
  Purpose: get the API, session manager, streaming and dashboard working end to end
  quickly, and provide a reference implementation for the contract test suite. It is not the
  security story of the project. Document it as such.

  Implementation notes:

         Talk to the Docker Engine API over its unix socket (podman's compatible socket also
         works). Configure the socket path in config.
         Create containers with: memory limit and equal swap limit (no swap), NanoCPUs,
         PidsLimit, all capabilities dropped, no-new-privileges, the default seccomp profile,
         network mode none, and a tmpfs for /tmp when requested.
         Egress: network mode none plus the same unix-socket egress proxy design as the
         native backend (section 11), bind-mounting the socket directory into the container.
         Exec uses ContainerExecCreate and ContainerExecAttach, demultiplexed with
         stdcopy. Files use CopyToContainer and CopyFromContainer (tar streams).
         Snapshot uses ContainerCommit. Declare it slow in Capabilities.
         Warm pool: pre-create and start containers that run a sleep process.
         Keep this backend under roughly 1500 lines. Do not gold-plate it.




                                                                                            Page 12 of 35
sandboxd: Build Specification




  9. Native runtime (the core of the project)
  This is the part that must be excellent. Implement it in internal/runtime/native/ with unit
  tests per component and the shared contract suite on top.


  9.1 Requirements and doctor checks
   cellbox doctor verifies, and prints a precise fix for each failure:

        Kernel version at least 5.14.
        cgroup v2 mounted, and a delegated, writable cgroup subtree with the cpu, memory
        and pids controllers (io optional).
        Unprivileged user namespaces allowed. Check /proc/sys/user/max_user_namespaces,
        and on Ubuntu 24.04 and newer check kernel.apparmor_restrict_unprivileged_userns
        and explain the profile or sysctl fix.
        Rootless overlayfs with the userxattr option works (attempt a real test mount inside a
        throwaway user and mount namespace).
        Seccomp available, devpts newinstance works, enough free disk, and /dev/kvm
        presence (informational only).


  9.2 Spike list (do these first, record results in docs/DECISIONS.md)
    1. Rootless overlay mount with userxattr inside a user namespace on the target kernels.
    2. Starting a child directly inside a cgroup with CLONE_INTO_CGROUP from Go
       (SysProcAttr.UseCgroupFD and CgroupFD).
    3. A unix SOCK_SEQPACKET control channel that passes file descriptors with
       SCM_RIGHTS using golang.org/x/sys/unix.
    4. Seccomp filter installation with TSYNC via go-seccomp-bpf.
    5. A private devpts instance inside the sandbox without root.
    6. The Ubuntu 24.04 AppArmor user namespace restriction and what the doctor should
       tell users.


  9.3 Spawn sequence (host side)
    1. Create the session directory under the data dir with subdirectories upper, work and
       run.
    2. Create the cgroup for the session, write its limits, and open a file descriptor to the
       directory.




                                                                                            Page 13 of 35
sandboxd: Build Specification




    3. Create the control socketpair (AF_UNIX, SOCK_SEQPACKET).
    4. Start the child by re-executing /proc/self/exe with argv[0] set to cellbox-init. main()
       must dispatch on argv[0] before any CLI parsing. The child gets: ExtraFiles of the
       control socket and a config pipe; a minimal environment; SysProcAttr with Cloneflags
       for NEWUSER, NEWNS, NEWPID, NEWNET, NEWIPC, NEWUTS and NEWCGROUP;
       UidMappings and GidMappings of one entry each mapping container id 0 to the host
        uid and gid (setgroups denied); UseCgroupFD with the cgroup fd; Pdeathsig SIGKILL;
        Setsid true.
    5. Because the child is created by clone with the flags, all namespaces exist from its first
        instruction, which avoids the multithreaded unshare problems of the Go runtime.
    6. Send the init configuration as JSON over the config pipe, never in argv: rootfs lower
       directories, upper and work directories, hostname, environment, rlimits, tmpfs sizes,
        seccomp profile name, and the run directory for the egress socket.
    7. Wait for the ready message on the control channel with a 5 second timeout. On failure
       kill the cgroup and surface the child's stderr in the error.

  The mapping choice is deliberate. A single-id mapping needs no setuid helpers
  (newuidmap) and no subuid ranges. The workload appears as root inside the sandbox but
  has zero capabilities, and all files in the sandbox appear root-owned. Document this
  trade-off. A full subuid-range mapping is a possible later enhancement, not part of v1.


  9.4 Init stage (PID 1 inside the sandbox)
  The init process performs setup, then becomes the supervisor that serves the control
  channel. In order:

    1. Set no_new_privs. Make the whole mount tree private (mount with MS_REC and
       MS_PRIVATE on /).
    2. Mount the overlay for the new root with lowerdir, upperdir, workdir and the userxattr
       option, with nosuid and nodev.
    3. Bind-mount the session run directory into the new root at /run/cellbox (the egress
       socket will live there).
    4. Mount a fresh procfs at /proc in the new root with nosuid, nodev and noexec. Mask
       sensitive paths: bind /dev/null over /proc/kcore, /proc/keys, /proc/timer_list and
       /proc/sched_debug, and mount empty read-only tmpfs over /proc/acpi, /proc/scsi and
       /sys/firmware where present. Remount /proc/sys, /proc/sysrq-trigger, /proc/irq and
        /proc/bus read-only by bind remount.




                                                                                           Page 14 of 35
sandboxd: Build Specification




    5. Build /dev as a small tmpfs: bind-mount null, zero, full, random, urandom and tty from
       the host /dev, mount a private devpts with newinstance and ptmxmode 0666, symlink
       /dev/ptmx to pts/ptmx, create /dev/shm as a size-limited tmpfs, and add the fd, stdin,
       stdout and stderr symlinks.
    6. Do not mount /sys by default. Provide an empty /sys directory.
    7. pivot_root into the new root using the pivot_root of the root onto itself pattern, then
       detach the old root with MNT_DETACH and chdir to /.
    8. Set the hostname to a short form of the session id. Bring the loopback interface up
       with an ioctl.
    9. Start the in-sandbox egress forwarder: a goroutine that listens on 127.0.0.1:3128 and, for
       each connection, dials the unix socket at /run/cellbox/egress.sock and copies bytes
       both ways. Dial lazily per connection so it works after warm pool claim.
 10. Drop all capabilities: clear the bounding set (PR_CAPBSET_DROP for every capability)
     and clear the effective, permitted, inheritable and ambient sets.
 11. Mark the init process non-dumpable (PR_SET_DUMPABLE 0) so workload processes of
     the same uid cannot read its memory through /proc.
 12. Send ready, then enter the control loop.


  9.5 Workload launcher (second stage)
  Init never executes user commands directly. For each exec it starts a short-lived helper by
  running /proc/self/exe with argv[0] set to cellbox-exec (this path works inside the
  sandbox because the kernel follows the magic link), passing the request over a pipe. The
  helper:

    1. Becomes a session leader and sets the controlling terminal if a PTY is requested.
    2. Sets cwd, environment and rlimits (nofile, core 0, fsize cap, optionally nproc).
    3. Closes every descriptor above 2 with close_range, which guarantees the workload
       never holds the control socket.
    4. Verifies the capability sets are empty, sets no_new_privs, and installs the seccomp
       filter with TSYNC.
    5. Calls execve. If execve fails, it writes the errno to a close-on-exec pipe so init can
        report a precise error (command not found, permission denied) instead of a generic
        failure.




                                                                                                Page 15 of 35
sandboxd: Build Specification




  The seccomp filter is a default-allow-minus-denylist profile in v1, defined in code as data
  and documented line by line in docs/SECCOMP.md. Deny at least: ptrace,
  process_vm_readv and process_vm_writev, kexec_load, init_module, finit_module,
  delete_module, bpf, perf_event_open, mount, umount2, pivot_root, chroot, swapon,
  swapoff, reboot, setns, unshare (user namespace creation), open_by_handle_at,
  name_to_handle_at, add_key, request_key, keyctl, userfaultfd, iopl, ioperm, and clone or
  clone3 with namespace flags. Return EPERM, not a kill, so that ordinary programs fail
  gracefully. Provide a documented strict profile as an option. Every denied call has a test.


  9.6 Control channel protocol
  Messages are small JSON documents over SOCK_SEQPACKET, with file descriptors
  attached through SCM_RIGHTS where needed. Define versioned message types in
  internal/runtime/native/proto:

        ready, error.
        exec (id, argv, cwd, env, tty, rows, cols, rlimits) answered by exec_started (id, pid)
        carrying the stdin write end and the stdout and stderr read ends, or a single PTY
        master. Later exec_exit (id, exit code or signal, reason).
        signal (id, signal number) and resize (id, rows, cols).
        fs_open_read, fs_open_write, fs_list, fs_tar_create and fs_tar_extract (see 9.8).
        ping and shutdown.

  The worker reads and writes the returned descriptors itself, so output never has to be
  proxied through the init process. This keeps init small and fast.


  9.7 Exec and PTY handling
        Non-TTY exec: separate stdout and stderr pipes, each pumped by a goroutine into the
        exec output log with per-chunk sequence numbers and timestamps.
        TTY exec: a single PTY master. Resize is an ioctl on the master performed by the
        worker. Keep a 256 KiB scrollback ring buffer per terminal for reattach.
        Backpressure: a slow SSE client must never block the process or the log writer. The log
        writer is the single source of truth. Clients read from the log with their own cursor and
        are dropped, not buffered without bound, if they lag more than a configured amount.
        Output cap: when the cap is reached, stop logging, set the truncated flag, and keep
        draining the pipes so the process does not block. Optionally kill the process when
        kill_on_output_cap is set.




                                                                                                 Page 16 of 35
sandboxd: Build Specification




        Timeouts: on timeout send SIGTERM to the exec process group, wait 2 seconds, then
        SIGKILL the group. Descendants that escaped the process group with setsid survive
        until the session ends or is killed. Document this. Offer a per-request option that kills
        the entire session instead.
        Zombies: init is PID 1 and must reap all children, including orphans reparented to it.


  9.8 File operations (security critical)
  All file I/O happens inside the sandbox, performed by the init process, never by the host
  reading the overlay directories. Path resolution therefore happens in the sandbox's own
  root, and symlink tricks cannot point at host files. Rules:

        Reads: init opens the file with O_NONBLOCK, calls fstat, rejects anything that is not a
        regular file (FIFOs, sockets and devices are refused with a coded error), enforces the
        size cap, and returns the descriptor over the control channel.
        Writes: create parent directories, write to a temporary name in the same directory,
        fsync, then rename into place, so a failed upload never leaves a half-written file.
        Directory listing returns name, type, size, mode and modification time, with a cap on
        entries and pagination.
        Archive create and extract run inside init using archive/tar. Extraction sanitizes every
        entry: reject absolute paths and any path containing .. after cleaning, refuse to follow
        symlinks while resolving parent directories (use openat2 with
        RESOLVE_NO_SYMLINKS and RESOLVE_BENEATH on a directory descriptor for the
        target), clamp modes, drop setuid and setgid bits, skip device nodes, enforce total size
        and entry count caps, and defend against tar bombs.
        A hostile workload can create FIFOs, huge sparse files and symlink loops specifically to
        hang or confuse init. Every operation has a deadline and a size cap. Add tests for each
        of these tricks.


  9.9 Kill and cleanup
        Kill writes 1 to the session cgroup.kill file, which atomically kills the whole process tree,
        then waits for the cgroup to report not populated (cgroup.events, via inotify).
        Destroy: kill, wait, remove the cgroup directory, remove the session directory. Mounts
        disappear with the mount namespace. If removal fails on restrictive modes, chmod the
        tree and retry. A background janitor retries failed removals.
        Reconcile on startup: list cgroups under the cellbox subtree, kill any not owned by a
        live session, mark orphaned sessions as lost, and clean their directories.



                                                                                               Page 17 of 35
sandboxd: Build Specification




  10. Resource limits and cgroups

  Delegation and layout
  Discover the service's own cgroup from /proc/self/cgroup. It must be delegated and
  writable (run under a systemd unit with Delegate=yes, or via systemd-run --user --scope
  with the Delegate property). Because cgroup v2 forbids enabling controllers for children
  while the cgroup itself has member processes, the service must first move itself into a
  leaf cgroup named main, then enable the cpu, memory, pids and io controllers by writing
  to cgroup.subtree_control of the delegated cgroup. Session cgroups live under a sessions
  subdirectory. Doctor verifies this whole chain.


  Limits written per session
        memory.max from memory_mb. memory.swap.max set to 0. memory.oom.group set to 1
        so an OOM kills the entire group, not one process. Optionally memory.high at 90
        percent to throttle before killing.
        pids.max from the pids limit.
        cpu.max as a quota and a 100000 microsecond period computed from millicores.
        cpu.weight 100.
        io.max or io.weight only if configured for a specific block device.
        UpdateLimits rewrites these live. The warm pool relies on this to apply the real limits at
        claim time.


  OOM and breach detection
  Watch memory.events with inotify. When oom_kill increases, record a limit.breach audit
  event with kind memory, and make the running execs finish with reason oom_killed
  (distinct from timed_out and from a normal signal). Do the same for pids.events max
  counts.


  Metrics sampling
  Every second per active session read memory.current, memory.peak (kernel 5.19 or newer,
  otherwise track the maximum yourself), cpu.stat usage_usec (derive percent from
  deltas), pids.current and io.stat. Keep a 15 minute in-memory ring per session, serve it
  through the metrics endpoints, and export only low-cardinality aggregates to Prometheus
  (never a label per session id).




                                                                                            Page 18 of 35
sandboxd: Build Specification




  Disk
  Rootless mode cannot use filesystem project quotas. Implement three layers:

    1. A hard per-process file size rlimit (RLIMIT_FSIZE) on every workload.
    2. A watchdog that measures upper directory usage on an interval (fast walk with a
       cache, 2 second default) and, when the session exceeds disk_mb, freezes the cgroup,
        reports limit.breach with kind disk, and kills the session or fails writes according to
        policy.
    3. Admission control: refuse new sessions when host free space falls below a
        configurable floor.

  Also offer an ephemeral option that puts the upper layer on a size-limited tmpfs, which
  enforces the limit exactly (and counts against the memory cgroup) but disables
  snapshots for that session. When the service runs as root, support xfs or ext4 project
  quotas as a documented production option, and say so. Document that the default
  watchdog is a soft limit with a small overshoot window.


  11. Network policy and egress proxy

  Modes
        none (default): the network namespace has only loopback. There is no route out and
        no DNS.
        allowlist: only listed hosts and ports are reachable through the egress proxy.
        internet: any public address is reachable through the proxy, with private ranges still
        blocked. Admin config can disable this mode.


  Design
  The sandbox never gets a real network interface. The in-sandbox forwarder (section 9.4)
  tunnels to a per-session unix socket owned by the host-side proxy. The environment
  variables HTTP_PROXY, HTTPS_PROXY, http_proxy and https_proxy point at 127.0.0.1:3128
  and NO_PROXY is localhost,127.0.0.1. Programs that ignore the proxy variables simply
  have no network. This fails closed by construction, needs no root, no veth devices and no
  firewall rules.

  Proxy behavior:




                                                                                             Page 19 of 35
sandboxd: Build Specification




        Supports HTTP CONNECT (for HTTPS, no interception, no certificate tricks) and plain
        HTTP forwarding with absolute-form request lines.
        Normalizes hostnames: lowercase, IDNA to punycode, trailing dot removed. Wildcards
        like *.example.com match subdomains but not the apex.
        Parses IP literals strictly with net/netip and rejects ambiguous encodings (decimal,
        octal and hex forms).
        Resolves the name itself with timeouts, validates every returned address, then dials
        the validated IP directly. A net.Dialer Control hook re-checks the actual remote
        address at connect time as defense in depth against rebinding races.
        Blocks always, regardless of policy: loopback, unspecified, link-local (including
        169.254.169.254 cloud metadata), RFC 1918, CGNAT 100.64.0.0/10, IPv6 unique local and
        link-local, multicast, broadcast, and IPv4-mapped IPv6 forms of all of these. Blocked
        ranges are configurable only by admins, and only to add entries.
        Does not follow redirects (the client does, so every hop is checked again).
        Per-session limits: concurrent connections, idle timeout, header size, optional
        bandwidth cap. Defends against slowloris.
        Logs every decision to egress_log with host, port, resolved IP, verdict, a stable reason
        code, byte counts and duration. Denials return 403 with an X-Cellbox-Deny-Reason
        header.

  Documented limitations: only HTTP and HTTPS through the proxy, no UDP, no raw sockets,
  no inbound connections. The default image set includes pip, npm and git configured to
  honor the proxy variables.

  Required tests: metadata IP, localhost, ::1, every private range, IPv4-mapped IPv6, tricky IP
  encodings, DNS rebinding (a test resolver that answers public first and private second),
  redirect to a private address, disallowed port, wildcard edge cases, oversized headers and
  slowloris, and a program that ignores proxy variables (must have no connectivity).


  12. Images, filesystem and snapshots

  Images
        Pull with go-containerregistry for the host architecture, anonymous by default with
        optional credentials from a docker-style config file. Verify digests and record the
        resolved digest on every session.




                                                                                              Page 20 of 35
sandboxd: Build Specification




        Flatten layers in order into images/<digest>/rootfs, applying OCI whiteouts (.wh.name
        deletes, .wh..wh..opq clears a directory). Unpack into a temporary directory and
        rename atomically. Deduplicate concurrent pulls with singleflight.
        Unpack safely: reject absolute and parent-traversal paths, never follow symlinks while
        placing files, strip setuid and setgid bits, skip device nodes, keep hardlinks within the
        tree, ignore ownership (everything becomes owned by the service user and appears as
        root in the sandbox), and enforce size caps.
        Garbage collect with reference counts and LRU, plus an admin prune endpoint.
        Ship configuration for curated defaults: python:3.12-slim, node:22-slim, golang:1.23 and
        debian:bookworm-slim.


  Session filesystem
  Each session mounts an overlay: the image rootfs (plus any snapshot layers) as read-only
  lowerdirs and a per-session upper and work directory. The whole root is writable through
  the overlay so that package installs work. /dev and /proc are as described in section 9.4.


  Snapshots and forks
  A live overlay's upper directory cannot be swapped, so snapshots are copies:

    1. Freeze the session cgroup (cgroup.freeze).
    2. Copy the upper directory into layers/<snapshot id>, preserving overlay metadata (the
       user.overlay.* extended attributes and whiteout markers) with lgetxattr and lsetxattr.
       Use reflink cloning (FICLONE) when the filesystem supports it and fall back to a plain
       copy otherwise. Reading the upper directory while frozen is safe.
    3. Thaw. The session continues untouched on its original upper.

  A fork creates a new session whose lowerdirs are the snapshot layer followed by its
  ancestors and then the image rootfs. When the chain would exceed 8 layers, flatten it:
  mount the chain read-only inside a helper user and mount namespace and copy the
  merged view into a single standalone layer. This resolves whiteouts correctly and keeps
  the mount option length bounded.

  Snapshots capture the filesystem only, not running processes or memory. Say so
  everywhere the feature is described. Snapshot size, creation time and parent are
  recorded. Deleting a snapshot that has children or live forks is refused with a coded error,
  unless forced.




                                                                                           Page 21 of 35
sandboxd: Build Specification




  13. Warm pool and scheduling

  Warm pool
        Per image, keep N pre-created sandboxes (default 2 for the curated images when
        enabled), within a global memory budget. Each is a fully initialized sandbox:
        namespaces up, overlay mounted, init ready, tiny memory limit.
        The run directory is bind-mounted at creation, and the egress socket is created inside
        it only at claim time. This works because the in-sandbox forwarder dials lazily.
        Claim is an atomic pop. On claim: apply the real limits through UpdateLimits, start the
        per-session egress proxy, bind the session id and hostname, and mark ready.
        Environment variables are applied at exec time.
        Refill asynchronously with jitter. Recycle entries older than 10 minutes. Never let the
        pool starve real sessions of memory.


  Admission control and scheduling (single node)
        Track committed memory, CPU millicores and pids against host capacity with
        configurable overcommit ratios (default 1.0 for memory, 4.0 for CPU).
        When capacity is short, either reject with a coded capacity_exceeded error and a
        Retry-After header, or wait in a bounded FIFO queue for up to a configurable time
        (default 5 seconds).
        Per-key quotas (max sessions, max total memory, requests per second) are enforced
        before capacity is considered.


  Cluster scheduler (phase 8 only)
  Workers join with a token over gRPC, send heartbeats and capacity reports, and the
  scheduler places sessions by free memory with spread as a tiebreaker. The gateway
  proxies every request for a session to its owning worker. Workers support drain mode. A
  lost worker marks its sessions lost. Postgres replaces SQLite behind the Store interface.
  Do not start this phase until single-node mode meets every acceptance criterion.




                                                                                           Page 22 of 35
sandboxd: Build Specification




  14. Observability, audit and replay

  Logging
  Structured slog output with request_id, session_id and exec_id on every relevant line.
  JSON in production, text in development. Never log exec output, file contents, API key
  secrets, Authorization headers or environment variable values. A redaction helper is
  applied at the logger boundary and covered by tests.


  Metrics (Prometheus, low cardinality)
        cellbox_sessions (gauge by state), cellbox_session_create_seconds (histogram, label
        source with values warm and cold), cellbox_exec_seconds (histogram),
        cellbox_exec_total (counter by result: ok, nonzero, timed_out, oom_killed, failed).
        cellbox_limit_breach_total (by kind: memory, pids, disk, output, timeout),
        cellbox_egress_requests_total (by verdict), cellbox_pool_ready (by image),
        cellbox_capacity_committed and cellbox_capacity_total (by resource),
        cellbox_output_bytes_total.
        Standard HTTP server metrics (requests, latency, in-flight) with route templates as
        labels, never raw paths.

  Tracing with OpenTelemetry is optional and off by default. Provide a Grafana dashboard
  JSON in deploy/grafana.


  Audit events
  Events are written in the same transaction as the state change they describe. They record
  the key id, never secrets. Retention is configurable (default 30 days). They are readable
  through the events endpoint and shown in the dashboard.


  Output logs
  Each exec writes an append-only file. Record framing: a 4-byte big-endian length, a 1-byte
  stream id (stdout, stderr, tty), an 8-byte nanosecond timestamp, then the payload. The
  SSE event id is the byte offset of the record. Writes are buffered and flushed every 100
  ms or 64 KiB. Readers tolerate a partial trailing record after a crash. Logs are deleted with
  their session after the retention period (default 7 days) and are subject to a total size cap.




                                                                                          Page 23 of 35
sandboxd: Build Specification




  Replay
  The replay endpoint returns a merged, timestamped sequence of output records and
  exec boundaries for a session. The dashboard plays it in an xterm instance with a
  scrubber and speed control. TTY execs can also be exported in the asciicast v2 format so
  recordings play in standard tools.


  15. Dashboard
  The dashboard is a Next.js 15 static export embedded in the Go binary with go:embed and
  served at /. It talks to the same public API as every other client. Build it as a real product,
  not a debug page.


  Auth
  The login page accepts an API key. The server answers with an HttpOnly, SameSite=Strict,
  Secure (when TLS) session cookie. Cookie-authenticated mutating requests must carry a
  CSRF header (double-submit token). No key is ever stored in localStorage.


  Pages
        Overview: capacity gauges, active sessions, create rate and error rate sparklines,
        warm pool status, recent limit breaches, doctor status.
        Sessions: a live table (updated by SSE) with state, image, age, CPU and memory
        sparklines, labels, filters and bulk destroy.
        Session detail, with tabs:
             Terminal: xterm.js with the WebGL addon, auto-reconnect, scrollback restore, resize
             handling, copy and paste.
             Timeline: every exec with a duration bar, exit badge, peak memory and a virtualized
             output viewer that handles 100k lines.
             Files: lazy tree, read-only code viewer (CodeMirror 6), drag and drop upload,
             download as tar.
             Metrics: uPlot charts for CPU, memory, pids and IO with the configured limits drawn
             as lines, live over SSE.
             Network: the egress log with verdict filter and per-host summary, plus a policy
             viewer.
             Snapshots: the snapshot tree with fork buttons.
             Events: the audit stream.


                                                                                             Page 24 of 35
sandboxd: Build Specification




        Replay: player with scrubber, speed control and jump-to-exec.
        Images, Keys, System (doctor results and capacity), and a read-only view of effective
        configuration.


  Quality bar
        Light and dark themes, keyboard navigation, a command palette, meaningful empty
        and error states (show the problem code and the fix), skeleton loading, and WCAG AA
        contrast.
        Initial JavaScript under 250 KB gzipped, with xterm.js and CodeMirror loaded lazily.
        The terminal sustains 60 fps under heavy output.
        Use TanStack Query for data, SSE for live data with automatic backoff, and a typed API
        client generated from api/openapi.yaml.
        Playwright end-to-end tests run against a real cellbox binary with the docker backend
        or native backend: login, create a session, run a command, see streamed output,
        upload and download a file, watch metrics move, fork a snapshot, play a replay.
        In development, the Next.js dev server proxies to the API on :8080. CORS is enabled
        only in development mode.


  16. SDKs, tool schemas, MCP server and CLI

  Python SDK (sdk/python)
  Sync and async clients on httpx, typed models, context managers that always destroy
  sessions, iterators for streaming output, automatic retries with idempotency keys on safe
  operations, and typed exceptions mapped from problem codes. Target feel:

     from cellbox import Client


     client = Client('http://127.0.0.1:8080', api_key='cbx_...')
     with client.sessions.create(image='python:3.12-slim', memory_mb=512) as box:
             box.write_file('/work/app.py', 'print(1 + 1)')
             result = box.exec(['python', '/work/app.py'], timeout_s=30)
             print(result.stdout, result.exit_code)
         for chunk in box.exec_stream(['sh', '-c', 'for i in 1 2 3; do echo $i;
     sleep 1; done']):
             print(chunk.text, end='')


  Publish with a pyproject, type hints (py.typed), and tests that run against a real server.



                                                                                          Page 25 of 35
sandboxd: Build Specification




  TypeScript SDK (sdk/typescript)
  fetch-based, ESM and CJS builds, AsyncIterable streaming, the same surface and error
  mapping. A thin Go client (sdk/go) reuses the server's types.


  Tool schemas for models (sdk/tools)
  Ship JSON schemas for run_command, write_file, read_file, list_files, snapshot and fork, in
  both the OpenAI-style function format and the Anthropic-style tool format, plus a helper
  that takes a model's tool call and dispatches it to the SDK. Tool results sent back to a
  model must be bounded: cap stdout and stderr at 16 KB each by keeping the head and tail
  with a clear omitted-bytes marker, and always include exit code, duration, and any
  timeout or OOM reason. Unbounded tool output is the most common way agents break
  their own context.


  MCP server
   cellbox mcp speaks the Model Context Protocol over stdio and over streamable HTTP. It
  lazily creates one session per MCP connection (conservative limits, network none by
  default, configurable through flags) and destroys it on disconnect. Tools: run_command,
  write_file, read_file, list_files, snapshot, fork and reset. Document the client configuration:
  the command is cellbox, the argument is mcp, and the environment variables
  CELLBOX_URL and CELLBOX_KEY select the server and key.


  CLI
   cellbox serve , doctor , keys create|ls|revoke , images pull|ls|rm , sessions
  ls|rm , run (create a session, run a command, stream, destroy), shell <session>
  (attach an interactive terminal), snapshot and fork , mcp , version . Support JSON
  output flags and shell completions.


  Examples (examples/)
    1. hosted-model: a tool-use loop against a hosted model API with the user's own key.
    2. ollama: the same loop against a local model, so the whole demo is free and offline.
    3. fix-loop: an agent receives a failing test, writes a fix, runs the tests in the sandbox and
       iterates. This is the README demo.
    4. eval-runner: runs a set of tasks in parallel, each fork-started from a shared base
        snapshot, collects results as JSON, and prints a summary. This demonstrates the
        evaluation use case.


                                                                                             Page 26 of 35
sandboxd: Build Specification




  Each example is under about 100 lines and has a deterministic fake-model mode so it can
  run in CI without any network or model.


  17. Security model

  Required wording
  Use this statement in README.md, SECURITY.md and docs/THREAT_MODEL.md,
  unchanged in meaning:

       The native backend isolates workloads using Linux user, mount, PID, network, IPC,
       UTS and cgroup namespaces, cgroup v2 resource limits, a seccomp filter and
       capability dropping. It shares the host kernel, so a kernel vulnerability can allow
       escape. For hostile multi-tenant workloads, run cellbox inside a dedicated virtual
       machine. Never expose the service to untrusted networks without TLS and the
       quotas and egress policy described in the documentation.


  Threat model document (docs/THREAT_MODEL.md)
  Structure it as: assets (host files, host secrets, the service's own credentials, other
  sessions, host availability, internal network), adversaries (a model-generated or injected
  malicious payload, a malicious API client with a valid session key, a network attacker),
  trust boundaries (client to API, API to worker, worker to init over the control channel,
  sandbox to host kernel, sandbox to network), and a table mapping each threat to its
  mitigation and its residual risk.

  In-scope threats and the control for each:

        Filesystem escape and host file access: mount namespace plus pivot_root, no host
        paths mounted except the run directory, file API executed inside the sandbox.
        Privilege escalation: user namespace with zero capabilities, no_new_privs, setuid bits
        stripped, nosuid mounts.
        Resource exhaustion (CPU, memory, pids, disk, output, file descriptors): cgroup limits,
        rlimits, watchdogs, output caps, admission control.
        Fork bombs and OOM: pids.max, memory.oom.group, cgroup.kill.
        Network abuse and SSRF: no interfaces, fail-closed egress proxy, blocked ranges, DNS
        pinning, full logging.




                                                                                             Page 27 of 35
sandboxd: Build Specification




        Cross-session interference: separate namespaces and cgroups, separate directories,
        session-scoped keys.
        Attacks on the supervisor (init) and the service: no control socket in workloads, non-
        dumpable init, ptrace denied, strict input validation, deadlines on every file operation.
        API abuse: authentication, quotas, rate limits, request size limits, idempotency,
        constant-time key comparison.

  Out of scope, stated plainly: kernel vulnerabilities, hardware side channels, a malicious
  host operator, and denial of service by an admin-level client.


  Hardening and supply chain
        Secure defaults everywhere: bind 127.0.0.1, network none, conservative limits, admin
        key printed once and stored only as a hash.
        Refuse non-loopback binds unless --allow-remote is set, and warn loudly when TLS is
        not configured.
        govulncheck, staticcheck, gosec and dependency review in CI. Pin GitHub Actions by
        commit. Generate an SBOM and sign release artifacts with cosign. Reproducible builds
        with -trimpath.
        Fuzz targets: tar extraction, path normalization, the proxy request parser, the control
        protocol decoder and the output log reader.
        SECURITY.md contains the disclosure process and the supported version policy.


  Security test suite (test/security)
  Each case runs inside a real sandbox and asserts the attack fails safely and the host stays
  healthy: writing to /proc/sys, mounting anything, unshare and setns, ptrace of init and of
  other processes, reading /proc/1/mem, enumerating and using inherited file descriptors
  above 2, loading modules, raw sockets, reaching the metadata IP and localhost services on
  the host, fork bomb, memory bomb, disk fill, file descriptor exhaustion, output flood,
  symlink and hardlink games through the file API, tar traversal and tar bombs, FIFO and
  special-file hangs on file reads, writing to the egress socket directory in ways that
  confuse the proxy, and reading the cellbox binary for secrets (there must be none to
  find).




                                                                                            Page 28 of 35
sandboxd: Build Specification




  18. Testing strategy
  Tests are part of the product. Use build tags so slow or privileged suites are opt-in locally
  and always on in CI: //go:build linux && integration and //go:build linux &&
  security . Tests that need a kernel feature the host lacks must skip with a clear reason,
  never fail silently or pass vacuously.

        Unit tests: table-driven, race detector on ( go test -race ), goroutine leak checks
        with go.uber.org/goleak, no sleeps for synchronization (use channels, fake clocks and
        polling helpers with deadlines).
        Runtime contract suite: shared across docker and native backends (section 7).
        Integration tests: start the real binary on a random port with a temp data dir and
        exercise the public API through the Go client: lifecycle, exec, streaming resume, files,
        quotas, idempotency, snapshots and forks, TTL reaping, error codes.
        API contract test: validate real responses against api/openapi.yaml.
        Security suite: section 17.
        Resource enforcement tests: allocate past the memory limit and assert oom_killed
        within a tolerance, spin CPU and assert the cgroup throttles to the configured quota
        within a tolerance, exceed pids, exceed disk, exceed output cap, exceed timeouts.
        Chaos tests: kill -9 the service mid-exec and assert reconcile cleans every cgroup and
        directory, kill init inside a sandbox, fill the host disk, disconnect clients mid-stream,
        restart repeatedly under load.
        Fuzzing: native Go fuzz targets for tar extraction, path normalization, the proxy parser,
        the control protocol decoder and the output log reader. Run for a fixed budget in CI
        and keep the corpus in the repo.
        State machine property tests: random sequences of create, exec, snapshot, kill, expire
        and destroy never reach an illegal state and never leak resources.
        Load and soak: a Go load generator in test/load measures create and exec latency
        percentiles at fixed concurrency and runs a one hour soak that watches for memory
        growth, file descriptor growth and leaked cgroups. Results feed
        docs/BENCHMARKS.md.
        Dashboard: Playwright end to end tests as in section 15.
        CI: GitHub Actions on ubuntu-24.04 with the AppArmor user namespace restriction
        disabled via sysctl for the integration and security jobs, gofmt and go vet, staticcheck,
        golangci-lint, govulncheck, build matrix for amd64 and arm64, and a release dry run.
        Coverage target: at least 75 percent of internal packages, with the security-critical
        packages (native, egress, auth) above 85 percent.


                                                                                             Page 29 of 35
sandboxd: Build Specification




  19. Packaging, installation and documentation

  Build and release
        A Makefile with: build, test, test-integration, test-security, web, lint, fuzz, bench,
        release-dry-run.
         make web builds the Next.js static export into the directory embedded by go:embed. A
        plain go build must work from a clean checkout, using a committed placeholder
        when the dashboard has not been built.
        goreleaser produces static linux/amd64 and linux/arm64 binaries with CGO disabled,
        checksums, an SBOM and cosign signatures.
        An install script downloads the right binary, verifies the checksum, and installs into
        ~/.local/bin with no sudo.
        Also provide: a systemd unit (system and user variants), a Docker image, and an Arch
        PKGBUILD for the AUR.


  systemd unit requirements
  Set Delegate=yes so the service owns a cgroup subtree, Restart=on-failure, a dedicated
  service user for the system unit, StateDirectory for the data dir, and sensible hardening
  (ProtectSystem=strict with the data dir writable, PrivateTmp, NoNewPrivileges,
  RestrictAddressFamilies limited to unix, inet and inet6). Document the user-unit variant
  for laptops.


  Container image caveat
  Running cellbox inside a Docker container requires nested user namespaces and a
  delegated cgroup, which the default Docker seccomp and AppArmor profiles block.
  Document the exact flags needed and state clearly that the recommended deployment is
  directly on a Linux host or VM. The compose file in deploy/ is for demos and says so.


  First run experience

     $ cellbox serve
       checking kernel and cgroups ... ok
       data dir: ~/.local/share/cellbox
         admin key (shown once): cbx_xxxxxxxx_...
         listening on 127.0.0.1:8080 (dashboard at /)




                                                                                                 Page 30 of 35
sandboxd: Build Specification




  If any prerequisite fails, print the doctor output with fixes and exit non-zero. Binding a
  non-loopback address requires --allow-remote.


  Configuration
  A single cellbox.yaml in ~/.config/cellbox (or /etc/cellbox), overridden by CELLBOX_
  environment variables, overridden by flags. Validate strictly and fail fast with precise
  messages. Keys:

     listen: 127.0.0.1:8080
     data_dir: ~/.local/share/cellbox
     backend: native            # native or docker
     limits:
       default: {cpu_millicores: 1000, memory_mb: 512, pids: 256, disk_mb: 1024,
     idle_ttl_s: 600, ttl_s: 3600}
       max:     {cpu_millicores: 4000, memory_mb: 4096, pids: 2048, disk_mb:
     10240, ttl_s: 86400}
     capacity:
       memory_overcommit: 1.0
       cpu_overcommit: 4.0
       queue_wait_s: 5
       min_free_disk_mb: 1024
     pool:
         enabled: true
         per_image: 2
       memory_budget_mb: 1024
     egress:
       allow_internet_mode: false
       extra_blocked_cidrs: []
     retention:
       output_days: 7
       audit_days: 30
     log: {level: info, format: json}
     tls: {cert: '', key: ''}



  Documentation set
  README.md (pitch, honest comparison with hosted sandboxes and plain Docker, sixty
  second quickstart, the fix-loop demo recording, security statement),
  docs/ARCHITECTURE.md, docs/API.md (generated from OpenAPI), docs/SECURITY.md,
  docs/THREAT_MODEL.md, docs/SECCOMP.md, docs/OPERATIONS.md (systemd,
  delegation, upgrades, backups, capacity planning), docs/BENCHMARKS.md,




                                                                                          Page 31 of 35
sandboxd: Build Specification




  docs/DECISIONS.md, docs/DEPENDENCIES.md, CONTRIBUTING.md and
  CHANGELOG.md. Versioning is semantic. The /v1 API is stable once released, and
  database migrations are forward-only.


  20. Phased build plan with acceptance criteria
  Do not start a phase until the previous phase's acceptance criteria are met. At the end of
  each phase update docs/PROGRESS.md with status, decisions taken, and known gaps.


  Phase 0: Foundations
  Repository, Go module, Makefile, CI, config loading and validation, slog setup with
  redaction, Store interface with the SQLite implementation and migrations, problem+json
  error package, api/openapi.yaml skeleton, doctor skeleton, ID generation.

  Acceptance: CI green on a clean checkout, cellbox version and cellbox doctor run,
  migrations apply and roll forward on an empty database, redaction tests pass.


  Phase 1: Docker backend and core API
  Runtime interface and contract suite, the Docker backend, auth and API keys, sessions,
  execs (wait mode and SSE), files, and the CLI commands serve, keys and run.

  Acceptance: the contract suite passes on the docker backend, the API contract test
  passes, and a documented curl walkthrough creates a session, runs a command with
  streamed output, uploads and downloads a file, and destroys the session.


  Phase 2: Reliability layer
  Session actors and state machine, TTL and idle reaping, quotas and rate limits,
  idempotency keys, output log and resumable streams, startup reconcile, capacity
  admission control, Prometheus metrics, audit events.

  Acceptance: killing the service with -9 mid-exec and restarting leaves no orphans and
  marks sessions lost, a dropped SSE client resumes with no gaps or duplicates, quota and
  capacity errors carry the correct codes, state machine property tests pass, and goroutine
  leak tests pass.




                                                                                       Page 32 of 35
sandboxd: Build Specification




  Phase 3: Agent-facing surface and dashboard
  Python and TypeScript SDKs, tool schemas with bounded outputs, the MCP server, the
  four examples with fake-model mode, and the dashboard (all pages in section 15 except
  Network, Snapshots and Replay, which are added when their features land).

  Acceptance: the Python SDK test suite passes against a real server, the fix-loop example
  runs deterministically in CI with the fake model and also works with a real model, the MCP
  server works from a standard MCP client, and the Playwright suite passes.


  Phase 4: Native runtime
  Image pull and flatten, the spawn sequence, init and workload launcher, control channel,
  cgroups and limits, OOM detection, PTY and exec, file operations, kill and cleanup,
  complete doctor, seccomp profile and docs.

  Acceptance: the shared contract suite passes on the native backend, the security suite
  (excluding egress cases) passes, resource enforcement tests pass within tolerance, no
  cgroup, mount or directory leaks after the chaos tests, and the first benchmark numbers
  are recorded.


  Phase 5: Egress proxy and network policy
  Per-session proxy, policy engine, blocklist, forwarder integration, egress log, dashboard
  Network tab, and the docker backend wired to the same proxy design.

  Acceptance: every required network test in section 11 passes, a program that ignores
  proxy variables has no connectivity, allowlisted pip and git operations work, and the
  egress log shows allow and deny decisions with reasons.


  Phase 6: Warm pool, snapshots, forks and replay
  Warm pool with claim and refill, snapshot via freeze and copy, fork and flatten, replay
  endpoint and asciicast export, dashboard Snapshots and Replay pages, the eval-runner
  example using forks.

  Acceptance: warm creation p50 under 60 ms and cold creation p50 under 400 ms on the
  reference machine (or the real numbers documented with an explanation), a forked
  session sees exactly the snapshot's files and none of the parent's later changes, flatten
  keeps layer depth bounded, and replay reproduces a recorded session.




                                                                                          Page 33 of 35
sandboxd: Build Specification




  Phase 7: Hardening, packaging and v1.0
  Fuzzing and security review pass, the complete threat model and documentation set,
  goreleaser pipeline, systemd units, the install script, the AUR package, benchmarks and
  the soak test, the demo recording, and a CHANGELOG.

  Acceptance: every item in the definition of done (section 21) is satisfied, the one hour
  soak shows no memory, descriptor or cgroup leaks, a fresh machine can go from zero to a
  working session in under five minutes by following the README.


  Phase 8: Cluster mode (optional)
  Postgres Store implementation, gRPC worker join and heartbeats, scheduler placement,
  request routing to the owning worker, drain and lost-worker handling.

  Acceptance: a gateway with three workers on one machine distributes sessions, survives
  a worker being killed (its sessions become lost, new sessions go elsewhere), and the full
  contract and integration suites pass through the gateway.


  21. Definition of done, quality bar and pitfalls

  Definition of done for v1.0
        Every acceptance criterion above is met and recorded in docs/PROGRESS.md.
        gofmt, go vet, staticcheck and golangci-lint are clean. The race detector is clean. CI is
        green on amd64 and arm64.
        Every exported symbol has a doc comment. Errors are wrapped with %w and map to
        stable problem codes. There are no panics in request paths, no global mutable state,
        and context cancellation is honored everywhere.
        Every create path has a matching undo path (use a cleanup stack so that a failure at
        step N unwinds steps N-1 to 1), and every destroy path is idempotent.
        The README makes no security claim that the threat model does not support.
        Published benchmark numbers are real measurements with the hardware and kernel
        noted.


  Pitfalls to avoid
        Do not call unshare for namespaces from a multithreaded Go process. Use clone flags
        through SysProcAttr and the re-exec pattern.




                                                                                             Page 34 of 35
sandboxd: Build Specification




        cgroup v2 forbids enabling controllers on a cgroup that has processes. Move the
        service into a leaf first.
        File descriptors passed over SCM_RIGHTS leak easily. Close them deterministically
        and test for descriptor growth.
        Init must reap zombies, and exec process groups must be handled carefully with PTYs.
        Reverse proxies buffer SSE. Send X-Accel-Buffering: no, flush after every event, and
        send heartbeats.
        SQLite in WAL mode allows one writer. Use a single write connection with a busy
        timeout and keep transactions short.
        Overlay metadata (user.overlay.* attributes) must be preserved when copying layers.
        Deleting trees created inside a user namespace can fail on restrictive modes. Chmod
        and retry.
        Hostile workloads attack the supervisor through the file API. Every operation needs
        deadlines, size caps and file type checks.
        Never trust sizes, paths, modes or counts that come from a tar stream or from a client.


  Working rules for the agent
        Finish a phase completely, with tests and docs, before starting the next.
        When a spike contradicts this spec, follow the evidence, write the decision into
        docs/DECISIONS.md, and adjust the affected sections of the spec in the same commit.
        If something in this spec is impossible on the target kernel, degrade gracefully, report it
        in doctor and in Capabilities, and document it. Never silently skip a security control.
        Ask the human only for product-level decisions (naming, licensing, scope changes).
        Everything else has a default in this document.
        Choose an open-source license (Apache-2.0 recommended) and add it in Phase 0.




                                                                                             Page 35 of 35

