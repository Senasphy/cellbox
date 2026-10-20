// Package doctor checks whether the current host can run cellbox backends.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const minimumFreeBytes = uint64(1 << 30)

// Check performs host prerequisite checks and prints a concrete remedy for failures.
func Check() []string {
	var out []string
	out = append(out, fmt.Sprintf("platform: %s/%s", runtime.GOOS, runtime.GOARCH))
	if runtime.GOOS != "linux" {
		return append(out, "native backend: unavailable (fix: run cellbox inside a Linux VM)")
	}

	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		out = append(out, "kernel version: unknown (fix: install Linux kernel 5.14 or newer)")
	} else if atLeastKernel(string(release), 5, 14) {
		out = append(out, "kernel version: "+strings.TrimSpace(string(release)))
	} else {
		out = append(out, "kernel version: "+strings.TrimSpace(string(release))+" (fix: upgrade to Linux 5.14 or newer)")
	}

	controllers, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		out = append(out, "cgroup v2: unavailable (fix: boot with a unified cgroup v2 hierarchy)")
	} else {
		_ = controllers // Its presence confirms the unified hierarchy is mounted.
		current, currentErr := currentCgroupDir()
		available, controllerErr := cgroupControllers(current)
		missing := missingFields(available, []string{"cpu", "memory", "pids"})
		if currentErr != nil || controllerErr != nil {
			missing = []string{"cpu", "memory", "pids"}
		}
		if len(missing) == 0 {
			out = append(out, "cgroup v2 controllers: cpu, memory, pids available")
		} else {
			out = append(out, "cgroup v2 controllers: missing "+strings.Join(missing, ", ")+" in the service cgroup (fix: delegate and enable these controllers in the service's parent cgroup)")
		}
		if currentErr == nil && writableCgroupDelegation(current) {
			out = append(out, "cgroup delegation: writable")
		} else {
			out = append(out, "cgroup delegation: not writable (fix: run cellbox in a systemd unit with Delegate=yes or a delegated user scope)")
		}
	}

	if maxUserNS, err := os.ReadFile("/proc/sys/user/max_user_namespaces"); err != nil {
		out = append(out, "user namespaces: setting unavailable (fix: enable unprivileged user namespaces)")
	} else if n, _ := strconv.Atoi(strings.TrimSpace(string(maxUserNS))); n < 1 {
		out = append(out, "user namespaces: disabled (fix: set user.max_user_namespaces to a positive value)")
	} else {
		out = append(out, "user namespaces: enabled")
	}
	if restricted, err := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns"); err == nil && strings.TrimSpace(string(restricted)) == "1" {
		out = append(out, "AppArmor restricts unprivileged user namespaces (fix: add a local AppArmor profile for cellbox, or set kernel.apparmor_restrict_unprivileged_userns=0 if your security policy permits)")
	}

	if ok, detail := namespaceMountProbe(`base=$(mktemp -d); trap 'umount "$base/merged" 2>/dev/null || true; rm -rf "$base"' EXIT; mkdir "$base/lower" "$base/upper" "$base/work" "$base/merged"; mount -t overlay overlay -o "lowerdir=$base/lower,upperdir=$base/upper,workdir=$base/work,userxattr" "$base/merged"; touch "$base/merged/probe"`); ok {
		out = append(out, "rootless overlayfs userxattr: works")
	} else {
		out = append(out, "rootless overlayfs userxattr: unavailable (fix: enable rootless overlayfs on this kernel/filesystem; probe: "+detail+")")
	}

	if actions, err := os.ReadFile("/proc/sys/kernel/seccomp/actions_avail"); err == nil && fieldsSet(string(actions))["allow"] {
		out = append(out, "seccomp: available")
	} else {
		out = append(out, "seccomp: unavailable (fix: use a kernel built with CONFIG_SECCOMP_FILTER)")
	}
	if ok, detail := namespaceMountProbe(`base=$(mktemp -d); trap 'umount "$base" 2>/dev/null || true; rmdir "$base" 2>/dev/null || true' EXIT; mount -t devpts devpts "$base" -o newinstance,ptmxmode=0666,mode=0620; test -e "$base/ptmx"`); ok {
		out = append(out, "private devpts newinstance: works")
	} else {
		out = append(out, "private devpts newinstance: unavailable (fix: enable devpts mounts in user namespaces; probe: "+detail+")")
	}

	var stat syscallStatfs
	if err := statfs("/", &stat); err != nil {
		out = append(out, "free disk space: unknown (fix: check the data filesystem)")
	} else if stat.available < minimumFreeBytes {
		out = append(out, fmt.Sprintf("free disk space: %d bytes (fix: free at least 1 GiB on the data filesystem)", stat.available))
	} else {
		out = append(out, fmt.Sprintf("free disk space: %d bytes", stat.available))
	}
	if _, err := os.Stat("/dev/kvm"); err == nil {
		out = append(out, "/dev/kvm: present (informational)")
	} else {
		out = append(out, "/dev/kvm: absent (informational; no fix required)")
	}
	return out
}

// atLeastKernel compares a Linux release's major/minor version to a minimum.
func atLeastKernel(release string, major, minor int) bool {
	version := strings.SplitN(strings.TrimSpace(release), ".", 3)
	if len(version) < 2 {
		return false
	}
	gotMajor, errMajor := strconv.Atoi(version[0])
	minorText := strings.TrimLeftFunc(version[1], func(r rune) bool { return r < '0' || r > '9' })
	gotMinor, errMinor := strconv.Atoi(minorText)
	if errMajor != nil || errMinor != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

func fieldsSet(value string) map[string]bool {
	set := make(map[string]bool)
	for _, field := range strings.Fields(value) {
		set[field] = true
	}
	return set
}

func missingFields(have map[string]bool, required []string) []string {
	var missing []string
	for _, field := range required {
		if !have[field] {
			missing = append(missing, field)
		}
	}
	return missing
}

func namespaceMountProbe(script string) (bool, string) {
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		return false, "unshare is not installed"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, unshare, "--user", "--map-root-user", "--mount", "--fork", "sh", "-c", "set -eu; "+script)
	if output, err := cmd.CombinedOutput(); err != nil {
		return false, strings.TrimSpace(string(output))
	}
	return true, ""
}

type syscallStatfs struct{ available uint64 }

func statfs(path string, target *syscallStatfs) error {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return err
	}
	target.available = fs.Bavail * uint64(fs.Bsize)
	return nil
}

func currentCgroupDir() (string, error) {
	current, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	var cgroupPath string
	for _, line := range strings.Split(string(current), "\n") {
		if strings.HasPrefix(line, "0::") {
			cgroupPath = strings.TrimPrefix(line, "0::")
			break
		}
	}
	if cgroupPath == "" {
		return "", fmt.Errorf("unified cgroup path not found")
	}
	return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(filepath.Clean(cgroupPath), string(filepath.Separator))), nil
}

func cgroupControllers(path string) (map[string]bool, error) {
	contents, err := os.ReadFile(filepath.Join(path, "cgroup.controllers"))
	if err != nil {
		return nil, err
	}
	return fieldsSet(string(contents)), nil
}

func writableCgroupDelegation(path string) bool {
	f, err := os.OpenFile(filepath.Join(path, "cgroup.subtree_control"), os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	child, err := os.MkdirTemp(path, "cellbox-doctor-")
	if err != nil {
		return false
	}
	return os.Remove(child) == nil
}
