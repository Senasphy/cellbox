//go:build linux

package native

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
)

func TestNativeResourceLimitsEnforced(t *testing.T) {
	if os.Getenv("CELLBOX_RUN_NATIVE_RESOURCE_CONTRACT") != "1" {
		t.Skip("set CELLBOX_RUN_NATIVE_RESOURCE_CONTRACT=1 inside a delegated cgroup scope to run native resource checks")
	}
	cgroupPath, err := CurrentCgroupPath()
	if err != nil {
		t.Fatal(err)
	}
	cgroups, err := InitializeCgroupRoot(cgroupPath, os.Getpid())
	if err != nil {
		t.Fatalf("initialize delegated cgroup: %v", err)
	}
	backend, err := NewRuntime(filepath.Join(t.TempDir(), "native-resource"), cgroups)
	if err != nil {
		t.Fatalf("create native runtime: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	image, err := backend.EnsureImage(ctx, "alpine:latest")
	if err != nil {
		t.Fatalf("ensure resource test image: %v", err)
	}
	create := func(t *testing.T, name string, limits cellruntime.Limits) *sandbox {
		t.Helper()
		box, createErr := backend.Create(ctx, cellruntime.SandboxSpec{
			ID: fmt.Sprintf("ses_resource_%s_%d", name, time.Now().UnixNano()), Image: image, Limits: limits, NetworkMode: "none",
		})
		if createErr != nil {
			t.Fatalf("create %s-limited sandbox: %v", name, createErr)
		}
		nativeBox := box.(*sandbox)
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			if destroyErr := nativeBox.Destroy(cleanupCtx); destroyErr != nil {
				t.Errorf("destroy %s-limited sandbox: %v", name, destroyErr)
			}
		})
		return nativeBox
	}
	start := func(t *testing.T, box *sandbox, argv []string, timeoutMS int) cellruntime.ExecResult {
		t.Helper()
		handle, startErr := box.Exec(ctx, cellruntime.ExecRequest{Argv: argv, TimeoutMS: timeoutMS, OutputLimit: 4096})
		if startErr != nil {
			t.Fatalf("start workload: %v", startErr)
		}
		for range handle.Output() {
		}
		result, waitErr := handle.Wait(ctx)
		if waitErr != nil && !errors.Is(waitErr, context.DeadlineExceeded) {
			t.Fatalf("wait for workload: %v", waitErr)
		}
		return result
	}
	t.Run("cpu quota throttles busy process", func(t *testing.T) {
		box := create(t, "cpu", cellruntime.Limits{CPUMillicores: 100, MemoryMB: 128, PIDs: 32})
		before := readCPUStatValue(t, box.cgroup.path, "nr_throttled")
		_ = start(t, box, []string{"/bin/sh", "-c", "yes >/dev/null"}, 1200)
		after := readCPUStatValue(t, box.cgroup.path, "nr_throttled")
		if after <= before {
			t.Fatalf("CPU throttle counter did not increase: before=%d after=%d", before, after)
		}
	})
	t.Run("process ceiling records limit hit", func(t *testing.T) {
		box := create(t, "pids", cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 128, PIDs: 16})
		result := start(t, box, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 40 ]; do /bin/sleep 1 & i=$((i+1)); done; wait"}, 8000)
		stats, statsErr := box.Stats(ctx)
		if statsErr != nil {
			t.Fatalf("read process-limit stats: %v", statsErr)
		}
		if !result.PIDsLimitHit || stats.PIDsMaxEvents == 0 {
			t.Fatalf("process ceiling not reported: result=%+v stats=%+v", result, stats)
		}
	})
	t.Run("memory ceiling reports oom kill", func(t *testing.T) {
		box := create(t, "memory", cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 32, PIDs: 32})
		result := start(t, box, []string{"/bin/dd", "if=/dev/zero", "of=/dev/shm/cellbox-oom", "bs=1M", "count=48"}, 15000)
		stats, statsErr := box.Stats(ctx)
		if statsErr != nil {
			t.Fatalf("read memory-limit stats: %v", statsErr)
		}
		if !result.OOMKilled || stats.OOMKills == 0 {
			t.Fatalf("memory ceiling not reported: result=%+v stats=%+v", result, stats)
		}
	})
}

func readCPUStatValue(t *testing.T, cgroupPath, key string) uint64 {
	t.Helper()
	file, err := os.Open(filepath.Join(cgroupPath, "cpu.stat"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || fields[0] != key {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		return value
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("cpu.stat missing %q", key)
	return 0
}
