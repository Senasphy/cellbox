//go:build linux

package native

import (
	"os"
	"os/exec"
	"testing"

	seccomp "github.com/elastic/go-seccomp-bpf"
	"golang.org/x/sys/unix"
)

func TestDefaultSeccompPolicyAssemblesAndDeniesRequiredCalls(t *testing.T) {
	filter := DefaultSeccompFilter()
	if filter.Flag != seccomp.FilterFlagTSync || !filter.NoNewPrivs {
		t.Fatalf("filter flags = %#v", filter)
	}
	if filter.Policy.DefaultAction != seccomp.ActionAllow || len(filter.Policy.Syscalls) != 1 {
		t.Fatalf("unexpected default policy: %#v", filter.Policy)
	}
	if err := filter.Policy.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	if _, err := filter.Policy.Assemble(); err != nil {
		t.Fatalf("assemble policy: %v", err)
	}
	denied := map[string]bool{}
	for _, name := range filter.Policy.Syscalls[0].Names {
		denied[name] = true
	}
	for _, name := range SeccompDenylist {
		if !denied[name] {
			t.Errorf("required syscall %q is not denied", name)
		}
	}
	cloneCondition := filter.Policy.Syscalls[0].NamesWithCondtions
	if len(cloneCondition) != 1 || cloneCondition[0].Name != "clone" || cloneCondition[0].Conditions[0].Value != unix.CLONE_NEWNS|unix.CLONE_NEWCGROUP|unix.CLONE_NEWUTS|unix.CLONE_NEWIPC|unix.CLONE_NEWUSER|unix.CLONE_NEWPID|unix.CLONE_NEWNET {
		t.Fatalf("clone namespace condition = %#v", cloneCondition)
	}
}

func TestSeccompFilterEnforcesEPERM(t *testing.T) {
	if os.Getenv("CELLBOX_SECCOMP_CHILD") == "1" {
		if err := InstallDefaultSeccomp(); err != nil {
			t.Fatalf("install TSYNC seccomp filter: %v", err)
		}
		_, _, errno := unix.Syscall(unix.SYS_PTRACE, uintptr(unix.PTRACE_TRACEME), 0, 0)
		if errno != unix.EPERM {
			t.Fatalf("ptrace returned %v, want EPERM", errno)
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestSeccompFilterEnforcesEPERM$")
	command.Env = append(os.Environ(), "CELLBOX_SECCOMP_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("seccomp child failed: %v\n%s", err, output)
	}
}

func TestStrictSeccompProfileDisablesProcessCreation(t *testing.T) {
	filter := StrictSeccompFilter()
	if len(filter.Policy.Syscalls) != 1 {
		t.Fatalf("unexpected strict policy: %#v", filter.Policy)
	}
	group := filter.Policy.Syscalls[0]
	if len(group.NamesWithCondtions) != 0 {
		t.Fatal("strict profile must deny every clone call")
	}
	denied := map[string]bool{}
	for _, name := range group.Names {
		denied[name] = true
	}
	for _, name := range []string{"clone", "fork", "vfork"} {
		if !denied[name] {
			t.Errorf("strict profile does not deny %s", name)
		}
	}
	if err := filter.Policy.Validate(); err != nil {
		t.Fatalf("validate strict policy: %v", err)
	}
	if _, err := filter.Policy.Assemble(); err != nil {
		t.Fatalf("assemble strict policy: %v", err)
	}
}
