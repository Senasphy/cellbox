# Native workload seccomp profile

Cellbox's v1 workload profile is default allow with `EPERM` for the calls listed in `internal/runtime/native/seccomp.go`. It is installed with `no_new_privs` and `SECCOMP_FILTER_FLAG_TSYNC`, so all threads in the workload receive the filter. The policy is data and its test checks that it validates, assembles, and includes every required denial.

The `clone` rule checks the flags argument for any namespace creation bit. `clone3` is denied outright: its flags are stored in a structure addressed by a pointer, which classic seccomp BPF cannot safely dereference. Programs that rely on `clone3` may need a compatibility fallback to `clone`.

The list covers tracing and cross-process memory access, kernel module and BPF loading, perf events, mount and root changes, swap and reboot operations, namespace changes, handle-based path access, kernel keyrings, userfaultfd, and direct I/O privilege calls. Additions to this list should have a corresponding policy test.

`StrictSeccompFilter` adds unconditional denials for `clone`, `fork`, and `vfork`. It is intended for a single-process task that does not create children; applications that use worker subprocesses will fail under it. Both profiles use the same TSYNC and `EPERM` behavior.
