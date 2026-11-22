//go:build linux

package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/runtime/contract"
)

func init() {
	if len(os.Args) == 0 || os.Args[0] != "cellbox-init" {
		return
	}
	if err := RunInit(3, 4); err != nil {
		fmt.Fprintln(os.Stderr, "cellbox init:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestRuntimeContract(t *testing.T) {
	if os.Getenv("CELLBOX_RUN_NATIVE_CONTRACT") != "1" {
		t.Skip("set CELLBOX_RUN_NATIVE_CONTRACT=1 inside a delegated cgroup scope to run the native runtime contract")
	}
	path, err := CurrentCgroupPath()
	if err != nil {
		t.Fatal(err)
	}
	cgroups, err := InitializeCgroupRoot(path, os.Getpid())
	if err != nil {
		t.Fatalf("initialize delegated cgroup: %v", err)
	}
	backend, err := NewRuntime(filepath.Join(t.TempDir(), "native"), cgroups)
	if err != nil {
		t.Fatalf("create native runtime: %v", err)
	}
	image := os.Getenv("CELLBOX_CONTRACT_IMAGE")
	if image == "" {
		image = "alpine:latest"
	}
	contract.RunBasic(t, backend, image)
}

func TestNativeNonePolicyBlocksDirectPublicNetwork(t *testing.T) {
	if os.Getenv("CELLBOX_RUN_NATIVE_CONTRACT") != "1" {
		t.Skip("set CELLBOX_RUN_NATIVE_CONTRACT=1 inside a delegated cgroup scope to run the native network check")
	}
	cgroupPath, err := CurrentCgroupPath()
	if err != nil {
		t.Fatal(err)
	}
	cgroups, err := InitializeCgroupRoot(cgroupPath, os.Getpid())
	if err != nil {
		t.Fatalf("initialize delegated cgroup: %v", err)
	}
	backend, err := NewRuntime(filepath.Join(t.TempDir(), "native-network"), cgroups)
	if err != nil {
		t.Fatalf("create native runtime: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	image, err := backend.EnsureImage(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("ensure network test image: %v", err)
	}
	box, err := backend.Create(ctx, cellruntime.SandboxSpec{
		ID: fmt.Sprintf("ses_net_none_%d_%d", os.Getpid(), time.Now().UnixNano()), Image: image,
		Limits: cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 256, PIDs: 64}, NetworkMode: "none",
	})
	if err != nil {
		t.Fatalf("create network-isolated sandbox: %v", err)
	}
	defer box.Destroy(context.Background())
	handle, err := box.Exec(ctx, cellruntime.ExecRequest{
		Argv: []string{"/bin/sh", "-c", "wget -T 3 -O /dev/null http://1.1.1.1/"}, TimeoutMS: 8000, OutputLimit: 4096,
	})
	if err != nil {
		t.Fatalf("start direct network probe: %v", err)
	}
	for range handle.Output() {
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("wait for direct network probe: %v", err)
	}
	if result.ExitCode == 0 || result.ExitCode == 124 {
		t.Fatalf("direct public network unexpectedly reachable or probe hung: %+v", result)
	}
}

func TestDiskQuotaStopsNativeSession(t *testing.T) {
	if os.Getenv("CELLBOX_RUN_NATIVE_DISK_CONTRACT") != "1" {
		t.Skip("set CELLBOX_RUN_NATIVE_DISK_CONTRACT=1 inside a delegated cgroup scope to run the native disk quota check")
	}
	cgroupPath, err := CurrentCgroupPath()
	if err != nil {
		t.Fatal(err)
	}
	cgroups, err := InitializeCgroupRoot(cgroupPath, os.Getpid())
	if err != nil {
		t.Fatalf("initialize delegated cgroup: %v", err)
	}
	backend, err := NewRuntime(filepath.Join(t.TempDir(), "native-disk"), cgroups)
	if err != nil {
		t.Fatalf("create native runtime: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	image, err := backend.EnsureImage(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("ensure disk test image: %v", err)
	}
	box, err := backend.Create(ctx, cellruntime.SandboxSpec{
		ID: fmt.Sprintf("ses_disk_contract_%d_%d", os.Getpid(), time.Now().UnixNano()), Image: image,
		Limits: cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 256, PIDs: 64, DiskMB: 1},
	})
	if err != nil {
		t.Fatalf("create disk-limited sandbox: %v", err)
	}
	defer box.Destroy(context.Background())
	if _, err := box.Exec(ctx, cellruntime.ExecRequest{
		Argv:      []string{"/bin/sh", "-c", "dd if=/dev/zero of=/tmp/cellbox-disk-limit bs=65536 count=32"},
		TimeoutMS: 15000, OutputLimit: 1024,
	}); err != nil {
		t.Fatalf("start disk usage command: %v", err)
	}
	source, ok := box.(interface {
		LimitEvents() <-chan cellruntime.LimitBreach
	})
	if !ok {
		t.Fatal("native sandbox does not expose limit events")
	}
	select {
	case event, open := <-source.LimitEvents():
		if !open || event.Kind != "disk" || event.Reason != "quota_exceeded" || event.UsedBytes <= event.LimitBytes {
			t.Fatalf("disk limit event=%+v open=%v", event, open)
		}
	case <-ctx.Done():
		t.Fatalf("disk quota watcher did not stop the session: %v", ctx.Err())
	}
	if _, err := box.Exec(ctx, cellruntime.ExecRequest{Argv: []string{"/bin/true"}}); err == nil {
		t.Fatal("native sandbox accepted an exec after disk quota termination")
	}
}
