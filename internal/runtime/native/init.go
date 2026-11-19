//go:build linux

package native

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const maxInitConfig = 1 << 20

// InitConfig contains trusted host paths and settings sent to the namespace init process.
type InitConfig struct {
	SessionID string            `json:"session_id"`
	LowerDirs []string          `json:"lower_dirs"`
	UpperDir  string            `json:"upper_dir"`
	WorkDir   string            `json:"work_dir"`
	RootDir   string            `json:"root_dir"`
	RunDir    string            `json:"run_dir"`
	Hostname  string            `json:"hostname"`
	Env       map[string]string `json:"env,omitempty"`
}

// RunInit reads the config from fd 4, prepares the sandbox root, then serves the control channel on fd 3.
func RunInit(controlFD, configFD int) error {
	controlFile := os.NewFile(uintptr(controlFD), "cellbox-control")
	configFile := os.NewFile(uintptr(configFD), "cellbox-init-config")
	if controlFile == nil || configFile == nil {
		return errors.New("native init requires control fd 3 and config fd 4")
	}
	defer controlFile.Close()
	defer configFile.Close()
	var config InitConfig
	decoder := json.NewDecoder(io.LimitReader(configFile, maxInitConfig))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return fmt.Errorf("decode native init config: %w", err)
	}
	if err := validateInitConfig(config); err != nil {
		return err
	}
	if err := setupSandboxRoot(config); err != nil {
		_ = SendControlMessage(controlFD, ControlMessage{Type: "error", Error: err.Error()})
		return err
	}
	// Install the filter only after namespace and mount setup, then keep the
	// filtered init alive so any future workload children inherit the policy.
	if err := InstallDefaultSeccomp(); err != nil {
		_ = SendControlMessage(controlFD, ControlMessage{Type: "error", Error: err.Error()})
		return fmt.Errorf("install native seccomp filter: %w", err)
	}
	if err := SendControlMessage(controlFD, ControlMessage{Type: "ready", ID: config.SessionID}); err != nil {
		return err
	}
	return controlLoop(controlFD)
}

func validateInitConfig(config InitConfig) error {
	if config.SessionID == "" || strings.ContainsAny(config.SessionID, `/\`) {
		return errors.New("native init config has an invalid session id")
	}
	if len(config.LowerDirs) == 0 || config.UpperDir == "" || config.WorkDir == "" || config.RootDir == "" || config.RunDir == "" {
		return errors.New("native init config is missing root filesystem paths")
	}
	for _, dir := range append(append([]string{}, config.LowerDirs...), config.UpperDir, config.WorkDir, config.RootDir, config.RunDir) {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
			return fmt.Errorf("native init path must be clean and absolute: %q", dir)
		}
	}
	if len(config.Hostname) > 63 {
		return errors.New("native init hostname exceeds 63 bytes")
	}
	return nil
}

func setupSandboxRoot(config InitConfig) error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mount tree private: %w", err)
	}
	if err := os.MkdirAll(config.RootDir, 0700); err != nil {
		return fmt.Errorf("create sandbox root mountpoint: %w", err)
	}
	options := "lowerdir=" + strings.Join(config.LowerDirs, ":") + ",upperdir=" + config.UpperDir + ",workdir=" + config.WorkDir + ",userxattr"
	if err := unix.Mount("overlay", config.RootDir, "overlay", unix.MS_NOSUID|unix.MS_NODEV, options); err != nil {
		return fmt.Errorf("mount sandbox overlay: %w", err)
	}
	if err := prepareRootMounts(config); err != nil {
		return err
	}
	if err := pivotSandboxRoot(config.RootDir); err != nil {
		return err
	}
	if config.Hostname != "" {
		if err := unix.Sethostname([]byte(config.Hostname)); err != nil {
			return fmt.Errorf("set sandbox hostname: %w", err)
		}
	}
	if err := bringLoopbackUp(); err != nil {
		return err
	}
	if err := dropCapabilities(); err != nil {
		return err
	}
	return nil
}

func prepareRootMounts(config InitConfig) error {
	root := config.RootDir
	for _, dir := range []string{"proc", "dev", "sys", "run/cellbox", "tmp", ".oldroot"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			return fmt.Errorf("create sandbox mountpoint %s: %w", dir, err)
		}
	}
	runTarget := filepath.Join(root, "run/cellbox")
	if err := unix.Mount(config.RunDir, runTarget, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("bind session run directory: %w", err)
	}
	if err := unix.Mount("proc", filepath.Join(root, "proc"), "proc", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return fmt.Errorf("mount sandbox procfs: %w", err)
	}
	for _, name := range []string{"kcore", "keys", "timer_list", "sched_debug"} {
		target := filepath.Join(root, "proc", name)
		if _, err := os.Stat(target); err == nil {
			if err := bindReadOnly("/dev/null", target); err != nil {
				return fmt.Errorf("mask /proc/%s: %w", name, err)
			}
		}
	}
	for _, name := range []string{"acpi", "scsi"} {
		target := filepath.Join(root, "proc", name)
		if _, err := os.Stat(target); err == nil {
			if err := mountTmpfsFlags(target, 1<<20, 0555, unix.MS_RDONLY); err != nil {
				return fmt.Errorf("mask /proc/%s: %w", name, err)
			}
		}
	}
	for _, name := range []string{"proc/sys", "proc/sysrq-trigger", "proc/irq", "proc/bus"} {
		target := filepath.Join(root, name)
		if _, err := os.Stat(target); err == nil {
			if err := bindReadOnly(target, target); err != nil {
				return fmt.Errorf("remount %s read-only: %w", name, err)
			}
		}
	}
	if err := mountTmpfsFlags(filepath.Join(root, "dev"), 64<<20, 0755, 0); err != nil {
		return fmt.Errorf("mount sandbox /dev: %w", err)
	}
	for _, dir := range []string{"pts", "shm"} {
		if err := os.MkdirAll(filepath.Join(root, "dev", dir), 0755); err != nil {
			return fmt.Errorf("create /dev/%s mountpoint: %w", dir, err)
		}
	}
	for _, name := range []string{"null", "zero", "full", "random", "urandom", "tty"} {
		target := filepath.Join(root, "dev", name)
		if err := removeExisting(target); err != nil {
			return fmt.Errorf("prepare /dev/%s mountpoint: %w", name, err)
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("create /dev/%s mountpoint: %w", name, err)
		}
		_ = file.Close()
		if err := unix.Mount(filepath.Join("/dev", name), target, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind /dev/%s: %w", name, err)
		}
	}
	pts := filepath.Join(root, "dev/pts")
	if err := unix.Mount("devpts", pts, "devpts", unix.MS_NOSUID|unix.MS_NOEXEC, "newinstance,ptmxmode=0666,mode=0620"); err != nil {
		return fmt.Errorf("mount private devpts: %w", err)
	}
	ptmx := filepath.Join(root, "dev/ptmx")
	if err := removeExisting(ptmx); err != nil {
		return fmt.Errorf("prepare /dev/ptmx: %w", err)
	}
	if err := os.Symlink("pts/ptmx", ptmx); err != nil {
		return fmt.Errorf("link /dev/ptmx: %w", err)
	}
	if err := mountTmpfs(filepath.Join(root, "dev/shm"), 64<<20, 01777); err != nil {
		return fmt.Errorf("mount /dev/shm: %w", err)
	}
	for name, target := range map[string]string{"fd": "/proc/self/fd", "stdin": "/proc/self/fd/0", "stdout": "/proc/self/fd/1", "stderr": "/proc/self/fd/2"} {
		link := filepath.Join(root, "dev", name)
		if err := removeExisting(link); err != nil {
			return fmt.Errorf("prepare /dev/%s: %w", name, err)
		}
		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("link /dev/%s: %w", name, err)
		}
	}
	// An empty read-only /sys also masks /sys/firmware when the image contains it.
	if err := mountTmpfsFlags(filepath.Join(root, "sys"), 1<<20, 0555, unix.MS_RDONLY); err != nil {
		return fmt.Errorf("provide empty /sys: %w", err)
	}
	return nil
}

func removeExisting(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return os.Remove(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func mountTmpfs(target string, size int64, mode os.FileMode) error {
	return mountTmpfsFlags(target, size, mode, 0)
}

func mountTmpfsFlags(target string, size int64, mode os.FileMode, extraFlags uintptr) error {
	data := fmt.Sprintf("size=%d,mode=%#o", size, uint32(mode)&07777)
	return unix.Mount("tmpfs", target, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|extraFlags, data)
}

func bindReadOnly(source, target string) error {
	if err := unix.Mount(source, target, "", unix.MS_BIND, ""); err != nil {
		return err
	}
	return unix.Mount("", target, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, "")
}

func pivotSandboxRoot(root string) error {
	oldRoot := filepath.Join(root, ".oldroot")
	if err := unix.Mount(root, root, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("make sandbox root a mountpoint: %w", err)
	}
	if err := unix.PivotRoot(root, oldRoot); err != nil {
		return fmt.Errorf("pivot into sandbox root: %w", err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("change to sandbox root: %w", err)
	}
	if err := unix.Unmount("/.oldroot", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("detach old root: %w", err)
	}
	if err := os.Remove("/.oldroot"); err != nil {
		return fmt.Errorf("remove old root mountpoint: %w", err)
	}
	return nil
}

func bringLoopbackUp() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open network ioctl socket: %w", err)
	}
	defer unix.Close(fd)
	request, err := unix.NewIfreq("lo")
	if err != nil {
		return err
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, request); err != nil {
		return fmt.Errorf("read loopback flags: %w", err)
	}
	request.SetUint16(request.Uint16() | unix.IFF_UP | unix.IFF_RUNNING)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, request); err != nil {
		return fmt.Errorf("bring loopback up: %w", err)
	}
	return nil
}

func dropCapabilities() error {
	lastCapBytes, err := os.ReadFile("/proc/sys/kernel/cap_last_cap")
	if err != nil {
		return fmt.Errorf("read cap_last_cap: %w", err)
	}
	lastCap, err := strconv.Atoi(strings.TrimSpace(string(lastCapBytes)))
	if err != nil || lastCap < 0 || lastCap > 255 {
		return fmt.Errorf("invalid cap_last_cap value %q", strings.TrimSpace(string(lastCapBytes)))
	}
	for cap := 0; cap <= lastCap; cap++ {
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(cap), 0, 0, 0); err != nil && err != unix.EINVAL {
			return fmt.Errorf("drop capability %d from bounding set: %w", cap, err)
		}
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil && err != unix.EINVAL {
		return fmt.Errorf("clear ambient capabilities: %w", err)
	}
	header := &unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := []unix.CapUserData{{}, {}}
	if err := unix.Capset(header, &data[0]); err != nil {
		return fmt.Errorf("clear process capabilities: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("disable init process dumps: %w", err)
	}
	return nil
}

func controlLoop(fd int) error {
	for {
		message, files, err := ReceiveControlMessage(fd)
		closeFiles(files)
		if err != nil {
			return err
		}
		switch message.Type {
		case "shutdown":
			return SendControlMessage(fd, ControlMessage{Type: "stopped", ID: message.ID})
		default:
			if err := SendControlMessage(fd, ControlMessage{Type: "error", ID: message.ID, Error: "unsupported init request: " + message.Type}); err != nil {
				return err
			}
		}
	}
}
