//go:build linux

package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/senasphy/cellbox/internal/runtime"
)

func TestCreateSessionCgroupWritesLimitsAndStats(t *testing.T) {
	parent := t.TempDir()
	limits := runtime.Limits{CPUMillicores: 500, MemoryMB: 256, PIDs: 42}
	cgroup, err := CreateSessionCgroup(parent, "ses_test", limits)
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{
		"cpu.max":          "50000 100000",
		"cpu.weight":       "100",
		"memory.max":       "268435456",
		"memory.high":      "241591910",
		"memory.swap.max":  "0",
		"memory.oom.group": "1",
		"pids.max":         "42",
	} {
		got, err := os.ReadFile(filepath.Join(parent, "ses_test", file))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, err=%v; want %q", file, got, err, want)
		}
	}
	for file, value := range map[string]string{"memory.current": "1234", "pids.current": "7", "cpu.stat": "usage_usec 99\nuser_usec 10\n"} {
		if err := os.WriteFile(filepath.Join(parent, "ses_test", file), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := cgroup.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.MemoryBytes != 1234 || stats.PIDs != 7 || stats.CPUUsageNS != 99_000 {
		t.Fatalf("stats = %+v", stats)
	}
	if err := cgroup.UpdateLimits(runtime.Limits{CPUMillicores: 1200, MemoryMB: 512, PIDs: 64}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateSessionCgroupRejectsUnsafeIDAndLimits(t *testing.T) {
	parent := t.TempDir()
	if _, err := CreateSessionCgroup(parent, "../escape", runtime.Limits{CPUMillicores: 1, MemoryMB: 1, PIDs: 1}); err == nil {
		t.Fatal("accepted path traversal in cgroup id")
	}
	if _, err := CreateSessionCgroup(parent, "ses_bad", runtime.Limits{}); err == nil {
		t.Fatal("accepted empty limits")
	}
	if _, err := os.Stat(filepath.Join(parent, "ses_bad")); !os.IsNotExist(err) {
		t.Fatalf("failed create left a cgroup directory: %v", err)
	}
}

func TestInitializeCgroupRootRequiresControllersAndCreatesHierarchy(t *testing.T) {
	root := t.TempDir()
	for file, value := range map[string]string{
		"cgroup.controllers":     "cpu memory pids io",
		"cgroup.procs":           "",
		"cgroup.subtree_control": "",
	} {
		if err := os.WriteFile(filepath.Join(root, file), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	service, err := InitializeCgroupRoot(root, 123)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "cgroup.procs")); string(got) != "123" {
		t.Fatalf("service cgroup.procs = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "cgroup.subtree_control")); string(got) != "+cpu +memory +pids +io" {
		t.Fatalf("root subtree controllers = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "sessions", "cgroup.subtree_control")); string(got) != "+cpu +memory +pids +io" {
		t.Fatalf("sessions subtree controllers = %q", got)
	}
	if _, err := service.Create("ses_test", runtime.Limits{CPUMillicores: 1000, MemoryMB: 256, PIDs: 64}); err != nil {
		t.Fatal(err)
	}
}

func TestInitializeCgroupRootRejectsMissingRequiredController(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("memory pids"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeCgroupRoot(root, 123); err == nil {
		t.Fatal("accepted delegated cgroup without cpu controller")
	}
}
