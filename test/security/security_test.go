//go:build linux && security

package security_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	nativeruntime "github.com/senasphy/cellbox/internal/runtime/native"
	"golang.org/x/sys/unix"
)

func init() {
	if len(os.Args) == 0 {
		return
	}
	switch filepath.Base(os.Args[0]) {
	case "cellbox-init":
		if err := nativeruntime.RunInit(3, 4); err != nil {
			fmt.Fprintln(os.Stderr, "cellbox init:", err)
			os.Exit(1)
		}
		os.Exit(0)
	case "cellbox-secprobe":
		if len(os.Args) < 2 {
			fmt.Fprintln(os.Stderr, "missing secprobe command")
			os.Exit(2)
		}
		if err := runSecProbe(os.Args[1], os.Args[2:]...); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func runSecProbe(cmd string, args ...string) error {
	switch cmd {
	case "write-proc-sys":
		err := os.WriteFile("/proc/sys/kernel/hostname", []byte("pwned"), 0644)
		if err == nil {
			return errors.New("unexpectedly wrote to /proc/sys/kernel/hostname")
		}
		return nil
	case "mount-tmpfs":
		err := unix.Mount("tmpfs", "/tmp", "tmpfs", 0, "")
		if err == nil {
			return errors.New("unexpectedly mounted tmpfs")
		}
		return nil
	case "unshare-user":
		if err := unix.Unshare(unix.CLONE_NEWUSER); err == nil {
			return errors.New("unexpectedly called unshare(CLONE_NEWUSER)")
		}
		if err := unix.Unshare(unix.CLONE_NEWNET); err == nil {
			return errors.New("unexpectedly called unshare(CLONE_NEWNET)")
		}
		return nil
	case "setns-self":
		f, err := os.Open("/proc/self/ns/net")
		if err != nil {
			return fmt.Errorf("open own netns: %w", err)
		}
		defer f.Close()
		if err := unix.Setns(int(f.Fd()), unix.CLONE_NEWNET); err == nil {
			return errors.New("unexpectedly called setns(CLONE_NEWNET)")
		}
		return nil
	case "ptrace-init":
		if err := unix.PtraceAttach(1); err == nil {
			return errors.New("unexpectedly ptrace-attached PID 1")
		}
		child := exec.Command("/bin/sleep", "2")
		if err := child.Start(); err != nil {
			return fmt.Errorf("start ptrace sibling: %w", err)
		}
		defer func() {
			_ = child.Process.Kill()
			_ = child.Wait()
		}()
		if err := unix.PtraceAttach(child.Process.Pid); err == nil {
			return errors.New("unexpectedly ptrace-attached child process")
		}
		return nil
	case "read-init-mem":
		f, err := os.Open("/proc/1/mem")
		if err == nil {
			_ = f.Close()
			return errors.New("unexpectedly opened /proc/1/mem")
		}
		return nil
	case "check-fds":
		for _, fd := range []int{3, 4, 5} {
			var buf [1]byte
			_, err := unix.Read(fd, buf[:])
			if err == nil || !errors.Is(err, unix.EBADF) {
				// FD 3 may briefly be the directory fd when ReadDir opens /proc/self/fd;
				// testing before ReadDir guarantees 3,4,5 must all be EBADF.
				return fmt.Errorf("fd %d unexpectedly usable: err=%v", fd, err)
			}
		}
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			return fmt.Errorf("list /proc/self/fd: %w", err)
		}
		if len(entries) > 4 {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return fmt.Errorf("unexpected inherited descriptors open: %v", names)
		}
		return nil
	case "init-module":
		if err := unix.InitModule([]byte("bad"), ""); err == nil {
			return errors.New("unexpectedly succeeded init_module")
		}
		if err := unix.FinitModule(-1, "", 0); err == nil {
			return errors.New("unexpectedly succeeded finit_module")
		}
		return nil
	case "raw-socket":
		fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, 0)
		if err == nil {
			_ = unix.Close(fd)
			return errors.New("unexpectedly opened AF_PACKET raw socket")
		}
		return nil
	case "fd-exhaust":
		var opened []*os.File
		defer func() {
			for _, f := range opened {
				_ = f.Close()
			}
		}()
		for i := 0; i < 2048; i++ {
			f, err := os.Open("/dev/null")
			if err != nil {
				return nil
			}
			opened = append(opened, f)
		}
		return errors.New("opened 2048 descriptors despite RLIMIT_NOFILE=1024")
	case "connect-ip":
		if len(args) < 1 {
			return errors.New("missing address")
		}
		conn, err := net.DialTimeout("tcp", args[0], 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return fmt.Errorf("unexpectedly connected directly to %s", args[0])
		}
		return nil
	case "proxy-get":
		if len(args) < 1 {
			return errors.New("missing url")
		}
		proxyURL, err := url.Parse("http://127.0.0.1:3128")
		if err != nil {
			return err
		}
		client := &http.Client{
			Timeout:   4 * time.Second,
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		}
		resp, err := client.Get(args[0])
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return fmt.Errorf("proxy unexpectedly returned 200 OK for %s", args[0])
		}
		return nil
	case "touch-egress-dir":
		if _, err := os.Stat("/run/cellbox/control.sock"); err == nil {
			return errors.New("control socket unexpectedly visible inside sandbox")
		}
		if err := os.WriteFile("/run/cellbox/cellbox-exec", []byte("pwned"), 0755); err == nil {
			return errors.New("unexpectedly overwrote /run/cellbox/cellbox-exec")
		}
		return nil
	case "check-env-secrets":
		for _, entry := range os.Environ() {
			if strings.Contains(entry, "cb_testsecret_") {
				return fmt.Errorf("secret leaked into sandbox env: %s", entry)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown secprobe subcommand %q", cmd)
	}
}

var (
	sharedOnce    sync.Once
	sharedBackend *nativeruntime.Runtime
	sharedCgroups *nativeruntime.CgroupRoot
	sharedCgroup  string
	sharedDataDir string
	sharedImage   cellruntime.ImageInfo
	sharedErr     error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedDataDir != "" {
		_ = os.RemoveAll(sharedDataDir)
	}
	os.Exit(code)
}

func sharedNativeEnv(t *testing.T) (*nativeruntime.Runtime, *nativeruntime.CgroupRoot, string, string, cellruntime.ImageInfo) {
	t.Helper()
	sharedOnce.Do(func() {
		sharedCgroup, sharedErr = nativeruntime.CurrentCgroupPath()
		if sharedErr != nil {
			return
		}
		sharedCgroups, sharedErr = nativeruntime.InitializeCgroupRoot(sharedCgroup, os.Getpid())
		if sharedErr != nil {
			sharedErr = fmt.Errorf("initialize delegated cgroup (run via systemd-run --user --scope -p Delegate=yes): %w", sharedErr)
			return
		}
		sharedDataDir, sharedErr = os.MkdirTemp("", "cellbox-sec-")
		if sharedErr != nil {
			return
		}
		sharedBackend, sharedErr = nativeruntime.NewRuntime(sharedDataDir, sharedCgroups)
		if sharedErr != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		sharedImage, sharedErr = sharedBackend.EnsureImage(ctx, "alpine:latest")
	})
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}
	return sharedBackend, sharedCgroups, sharedCgroup, sharedDataDir, sharedImage
}

func newDelegatedNativeRuntime(t *testing.T) (*nativeruntime.Runtime, cellruntime.ImageInfo) {
	t.Helper()
	backend, _, _, _, image := sharedNativeEnv(t)
	return backend, image
}

func installSecProbe(t *testing.T, ctx context.Context, box cellruntime.Sandbox) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	f, err := os.Open(exe)
	if err != nil {
		t.Fatalf("open test executable: %v", err)
	}
	defer f.Close()
	if err := box.WriteFile(ctx, "/tmp/secprobe", f, 0755); err != nil {
		t.Fatalf("upload secprobe into sandbox: %v", err)
	}
}

func execInBox(t *testing.T, ctx context.Context, box cellruntime.Sandbox, req cellruntime.ExecRequest) (cellruntime.ExecResult, string) {
	t.Helper()
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 15_000
	}
	if req.OutputLimit == 0 {
		req.OutputLimit = 16 << 10
	}
	handle, err := box.Exec(ctx, req)
	if err != nil {
		t.Fatalf("Exec(%v): %v", req.Argv, err)
	}
	var out strings.Builder
	for chunk := range handle.Output() {
		out.Write(chunk.Data)
	}
	res, err := handle.Wait(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait(%v): %v (output=%q)", req.Argv, err, out.String())
	}
	return res, out.String()
}

func TestNativeSecurityAttackMatrix(t *testing.T) {
	backend, image := newDelegatedNativeRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	hostTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("host-secret"))
	}))
	defer hostTarget.Close()
	hostAddr := strings.TrimPrefix(hostTarget.URL, "http://")

	box, err := backend.Create(ctx, cellruntime.SandboxSpec{
		ID:          fmt.Sprintf("ses_sec_none_%d", time.Now().UnixNano()),
		Image:       image,
		Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 256, PIDs: 64},
		NetworkMode: "none",
	})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	defer box.Destroy(context.Background())
	installSecProbe(t, ctx, box)

	// Re-exec /tmp/secprobe with argv[0]=cellbox-secprobe via sh -c 'exec -a ...' or symlink.
	res, out := execInBox(t, ctx, box, cellruntime.ExecRequest{
		Argv: []string{"/bin/ln", "-sf", "/tmp/secprobe", "/tmp/cellbox-secprobe"},
	})
	if res.ExitCode != 0 {
		t.Fatalf("create secprobe symlink failed: %+v %q", res, out)
	}

	probeCases := []struct {
		name string
		args []string
	}{
		{name: "write_proc_sys", args: []string{"write-proc-sys"}},
		{name: "mount_tmpfs", args: []string{"mount-tmpfs"}},
		{name: "unshare", args: []string{"unshare-user"}},
		{name: "setns", args: []string{"setns-self"}},
		{name: "ptrace_init_and_sibling", args: []string{"ptrace-init"}},
		{name: "read_proc_1_mem", args: []string{"read-init-mem"}},
		{name: "no_inherited_fds_above_2", args: []string{"check-fds"}},
		{name: "init_module_and_finit_module", args: []string{"init-module"}},
		{name: "raw_socket", args: []string{"raw-socket"}},
		{name: "fd_exhaustion_rlimit", args: []string{"fd-exhaust"}},
		{name: "metadata_ip_blocked_none", args: []string{"connect-ip", "169.254.169.254:80"}},
		{name: "host_loopback_blocked_none", args: []string{"connect-ip", hostAddr}},
		{name: "egress_dir_hardening", args: []string{"touch-egress-dir"}},
		{name: "no_secrets_in_sandbox_env", args: []string{"check-env-secrets"}},
	}
	for _, tc := range probeCases {
		t.Run(tc.name, func(t *testing.T) {
			argv := append([]string{"/tmp/cellbox-secprobe"}, tc.args...)
			r, o := execInBox(t, ctx, box, cellruntime.ExecRequest{Argv: argv})
			if r.ExitCode != 0 {
				t.Fatalf("probe %s failed (exit=%d): %s", tc.name, r.ExitCode, o)
			}
		})
	}

	t.Run("output_flood_capped", func(t *testing.T) {
		r, o := execInBox(t, ctx, box, cellruntime.ExecRequest{
			Argv:        []string{"/bin/sh", "-c", "dd if=/dev/zero bs=1024 count=64 2>/dev/null | tr '\\0' 'A'"},
			OutputLimit: 256,
		})
		if r.ExitCode != 0 || !r.Truncated || len(o) != 256 || r.OutputBytes != 65536 {
			t.Fatalf("output flood not properly capped: result=%+v len=%d", r, len(o))
		}
	})

	t.Run("binary_contains_no_test_secrets", func(t *testing.T) {
		var data []byte
		var err error
		for _, candidate := range []string{"bin/cellbox", filepath.Join("..", "..", "bin", "cellbox")} {
			data, err = os.ReadFile(candidate)
			if err == nil {
				break
			}
		}
		if err != nil {
			t.Skip("bin/cellbox not built")
		}
		// Construct sensitive prefixes dynamically so the literal never sits in this file.
		for _, prefix := range []string{"sk" + "-ant-api03-", "ghp" + "_", "AKIA" + "IOSFODNN7"} {
			if bytes.Contains(data, []byte(prefix)) {
				t.Fatalf("unexpected secret prefix %q in binary", prefix)
			}
		}
	})

	t.Run("internet_proxy_blocks_metadata_and_host_loopback", func(t *testing.T) {
		netBox, err := backend.Create(ctx, cellruntime.SandboxSpec{
			ID:            fmt.Sprintf("ses_sec_net_%d", time.Now().UnixNano()),
			Image:         image,
			Limits:        cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 256, PIDs: 64},
			NetworkPolicy: cellruntime.NetworkPolicy{Mode: "internet"},
		})
		if err != nil {
			t.Fatalf("create internet sandbox: %v", err)
		}
		defer netBox.Destroy(context.Background())
		installSecProbe(t, ctx, netBox)
		r, o := execInBox(t, ctx, netBox, cellruntime.ExecRequest{
			Argv: []string{"/bin/ln", "-sf", "/tmp/secprobe", "/tmp/cellbox-secprobe"},
		})
		if r.ExitCode != 0 {
			t.Fatalf("symlink secprobe failed: %+v %q", r, o)
		}
		for _, targetURL := range []string{"http://169.254.169.254/latest/meta-data/", hostTarget.URL} {
			r, o := execInBox(t, ctx, netBox, cellruntime.ExecRequest{
				Argv: []string{"/tmp/cellbox-secprobe", "proxy-get", targetURL},
			})
			if r.ExitCode != 0 {
				t.Fatalf("proxy SSRF check failed for %s: %+v %q", targetURL, r, o)
			}
		}
	})
}

func TestNativeFileAndArchiveSecurityMatrix(t *testing.T) {
	backend, image := newDelegatedNativeRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	box, err := backend.Create(ctx, cellruntime.SandboxSpec{
		ID:          fmt.Sprintf("ses_sec_files_%d", time.Now().UnixNano()),
		Image:       image,
		Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 256, PIDs: 64},
		NetworkMode: "none",
	})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	defer box.Destroy(context.Background())

	hostDir := t.TempDir()
	hostVictim := filepath.Join(hostDir, "host-victim.txt")
	if err := os.WriteFile(hostVictim, []byte("host-only"), 0600); err != nil {
		t.Fatal(err)
	}
	res, out := execInBox(t, ctx, box, cellruntime.ExecRequest{
		Argv: []string{"/bin/sh", "-c", fmt.Sprintf("ln -s %s /tmp/hostlink && ln -s %s /tmp/hostdirlink && ln -s /tmp/loop /tmp/loop && mkfifo /tmp/hangpipe && touch /tmp/real.txt && ln /tmp/real.txt /tmp/hard.txt", hostVictim, hostDir)},
	})
	if res.ExitCode != 0 {
		t.Fatalf("setup file traps failed: %+v %q", res, out)
	}

	if _, _, err := box.ReadFile(ctx, "/tmp/hostlink"); err == nil {
		t.Fatal("ReadFile followed symlink to host path")
	}
	_ = box.WriteFile(ctx, "/tmp/hostlink", bytes.NewBufferString("overwrite"), 0600)
	if got, err := os.ReadFile(hostVictim); err != nil || string(got) != "host-only" {
		t.Fatalf("host victim modified via symlink: %q err=%v", got, err)
	}
	if err := box.WriteFile(ctx, "/tmp/hostdirlink/pwn", bytes.NewBufferString("pwn"), 0600); err == nil {
		t.Fatal("WriteFile traversed symlink pointing to host directory")
	}
	if _, err := os.Stat(filepath.Join(hostDir, "pwn")); !os.IsNotExist(err) {
		t.Fatalf("WriteFile created file in host directory via symlink: %v", err)
	}
	if _, _, err := box.ReadFile(ctx, "/tmp/loop"); err == nil {
		t.Fatal("ReadFile succeeded on symlink loop")
	}

	fifoCtx, fifoCancel := context.WithTimeout(ctx, 2*time.Second)
	defer fifoCancel()
	if r, _, err := box.ReadFile(fifoCtx, "/tmp/hangpipe"); err == nil {
		_ = r.Close()
		t.Fatal("ReadFile opened a FIFO instead of rejecting it")
	}

	// Tar bomb / excessive entries and symlink-escape tar must be rejected before writing.
	var symTar bytes.Buffer
	tw := tar.NewWriter(&symTar)
	_ = tw.WriteHeader(&tar.Header{Name: "pre.txt", Mode: 0600, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("pre"))
	_ = tw.WriteHeader(&tar.Header{Name: "sub", Typeflag: tar.TypeSymlink, Linkname: "/etc"})
	_ = tw.WriteHeader(&tar.Header{Name: "sub/evil", Mode: 0600, Size: 4, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("evil"))
	_ = tw.Close()
	if err := box.Extract(ctx, "/tmp", bytes.NewReader(symTar.Bytes())); err == nil {
		t.Fatal("Extract accepted symlink + write through symlink in tar")
	}
	if _, _, err := box.ReadFile(ctx, "/tmp/pre.txt"); err == nil {
		t.Fatal("Extract wrote pre.txt before rejecting malicious entry")
	}
}

func TestNativeResourceBreachSecurity(t *testing.T) {
	backend, image := newDelegatedNativeRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("fork_bomb_contained_and_init_survives", func(t *testing.T) {
		box, err := backend.Create(ctx, cellruntime.SandboxSpec{
			ID:          fmt.Sprintf("ses_sec_fork_%d", time.Now().UnixNano()),
			Image:       image,
			Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 128, PIDs: 16},
			NetworkMode: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer box.Destroy(context.Background())

		res, _ := execInBox(t, ctx, box, cellruntime.ExecRequest{
			Argv:      []string{"/bin/sh", "-c", "i=0; while [ $i -lt 50 ]; do /bin/sleep 1 & i=$((i+1)); done; wait"},
			TimeoutMS: 8000,
		})
		stats, err := box.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !res.PIDsLimitHit || stats.PIDsMaxEvents == 0 {
			t.Fatalf("fork bomb not recorded: res=%+v stats=%+v", res, stats)
		}
		follow, out := execInBox(t, ctx, box, cellruntime.ExecRequest{Argv: []string{"/bin/echo", "alive"}})
		if follow.ExitCode != 0 || strings.TrimSpace(out) != "alive" {
			t.Fatalf("init unhealthy after fork bomb: res=%+v out=%q", follow, out)
		}
	})

	t.Run("memory_bomb_oom_kills_workload", func(t *testing.T) {
		box, err := backend.Create(ctx, cellruntime.SandboxSpec{
			ID:          fmt.Sprintf("ses_sec_oom_%d", time.Now().UnixNano()),
			Image:       image,
			Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 32, PIDs: 32},
			NetworkMode: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer box.Destroy(context.Background())

		res, _ := execInBox(t, ctx, box, cellruntime.ExecRequest{
			Argv:      []string{"/bin/dd", "if=/dev/zero", "of=/dev/shm/oom", "bs=1M", "count=48"},
			TimeoutMS: 15000,
		})
		stats, err := box.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OOMKilled || stats.OOMKills == 0 {
			t.Fatalf("memory bomb not OOM-killed: res=%+v stats=%+v", res, stats)
		}
	})

	t.Run("disk_fill_stops_session", func(t *testing.T) {
		box, err := backend.Create(ctx, cellruntime.SandboxSpec{
			ID:          fmt.Sprintf("ses_sec_disk_%d", time.Now().UnixNano()),
			Image:       image,
			Limits:      cellruntime.Limits{CPUMillicores: 1000, MemoryMB: 128, PIDs: 32, DiskMB: 1},
			NetworkMode: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer box.Destroy(context.Background())

		if _, err := box.Exec(ctx, cellruntime.ExecRequest{
			Argv:      []string{"/bin/sh", "-c", "dd if=/dev/zero of=/tmp/fill bs=65536 count=32"},
			TimeoutMS: 15000,
		}); err != nil {
			t.Fatal(err)
		}
		source := box.(interface {
			LimitEvents() <-chan cellruntime.LimitBreach
		})
		select {
		case ev, open := <-source.LimitEvents():
			if !open || ev.Kind != "disk" || ev.Reason != "quota_exceeded" {
				t.Fatalf("unexpected disk breach event: %+v open=%v", ev, open)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("timed out waiting for disk quota breach")
		}
	})
}

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err == nil {
			count++
		}
	}
	return count
}
