//go:build linux

package native

import (
	"github.com/elastic/go-seccomp-bpf"
	"golang.org/x/sys/unix"
)

// SeccompDenylist is the v1 default-allow profile. clone3 is denied because its
// flags live behind a userspace pointer and seccomp cannot inspect them.
var SeccompDenylist = []string{
	"ptrace", "process_vm_readv", "process_vm_writev", "kexec_load", "init_module",
	"finit_module", "delete_module", "bpf", "perf_event_open", "mount", "umount2",
	"pivot_root", "chroot", "swapon", "swapoff", "reboot", "setns", "unshare",
	"open_by_handle_at", "name_to_handle_at", "add_key", "request_key", "keyctl",
	"userfaultfd", "iopl", "ioperm", "clone3",
}

// DefaultSeccompFilter returns the v1 default-allow profile with EPERM denials.
func DefaultSeccompFilter() seccomp.Filter {
	names := append([]string(nil), SeccompDenylist...)
	group := seccomp.SyscallGroup{Names: names, Action: seccomp.ActionErrno}
	for _, name := range []string{"clone"} {
		group.NamesWithCondtions = append(group.NamesWithCondtions, seccomp.NameWithConditions{
			Name:       name,
			Conditions: seccomp.ArgumentConditions{{Argument: 0, Operation: seccomp.BitsSet, Value: unix.CLONE_NEWNS | unix.CLONE_NEWCGROUP | unix.CLONE_NEWUTS | unix.CLONE_NEWIPC | unix.CLONE_NEWUSER | unix.CLONE_NEWPID | unix.CLONE_NEWNET}},
		})
	}
	return seccomp.Filter{NoNewPrivs: true, Flag: seccomp.FilterFlagTSync, Policy: seccomp.Policy{
		DefaultAction: seccomp.ActionAllow,
		Syscalls:      []seccomp.SyscallGroup{group},
	}}
}

// StrictSeccompFilter additionally prevents a workload from creating child
// processes. It is suitable for single-process tasks that do not fork.
func StrictSeccompFilter() seccomp.Filter {
	filter := DefaultSeccompFilter()
	filter.Policy.Syscalls[0].Names = append(filter.Policy.Syscalls[0].Names, "clone", "fork", "vfork")
	filter.Policy.Syscalls[0].NamesWithCondtions = nil
	return filter
}

// InstallDefaultSeccomp installs the synchronized v1 syscall filter.
func InstallDefaultSeccomp() error {
	return seccomp.LoadFilter(DefaultSeccompFilter())
}
