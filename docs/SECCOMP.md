# Native workload seccomp profile

Cellbox's workload seccomp profile (`internal/runtime/native/seccomp.go`) is default-allow with `EPERM` for denied syscalls. It is installed with `no_new_privs` and `SECCOMP_FILTER_FLAG_TSYNC` so every thread in the workload inherits the filter.

- `clone` is inspected for any namespace-creation flags (`CLONE_NEW*`).
- `clone3` is denied outright because its flags live behind a userspace pointer that classic seccomp BPF cannot safely dereference. Programs that rely on `clone3` may need a fallback to `clone`.
- The denylist blocks tracing and cross-process memory access (`ptrace`, `process_vm_*`), kernel module and BPF loading, `perf_event_open`, mount/root changes (`mount`, `umount2`, `pivot_root`, `chroot`), swap/reboot, namespace changes (`setns`, `unshare`), handle-based path access, kernel keyrings, `userfaultfd`, and direct I/O privilege calls.

`StrictSeccompFilter` also denies `clone`, `fork`, and `vfork` unconditionally for single-process workloads that shouldn't spawn child processes.
