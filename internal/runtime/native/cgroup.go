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
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
)

const cgroupPeriod = 100_000

// CgroupRoot owns the service's delegated child hierarchy for native sessions.
type CgroupRoot struct {
	path        string
	controllers []string
}

// CurrentCgroupPath returns this process's unified cgroup-v2 directory.
func CurrentCgroupPath() (string, error) {
	contents, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", fmt.Errorf("read process cgroup membership: %w", err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if !strings.HasPrefix(line, "0::") {
			continue
		}
		relative := strings.TrimPrefix(line, "0::")
		if !filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
			return "", fmt.Errorf("invalid unified cgroup path %q", relative)
		}
		return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(relative, string(filepath.Separator))), nil
	}
	return "", errors.New("unified cgroup-v2 membership not found")
}

// InitializeCgroupRoot moves the service process into a leaf and enables delegated controllers.
func InitializeCgroupRoot(path string, servicePID int) (*CgroupRoot, error) {
	if err := validateServiceCgroupProcesses(path, servicePID); err != nil {
		return nil, err
	}
	available, err := cgroupControllers(path)
	if err != nil {
		return nil, fmt.Errorf("read delegated cgroup controllers: %w", err)
	}
	required := []string{"cpu", "memory", "pids"}
	if missing := missingCgroupControllers(available, required); len(missing) > 0 {
		return nil, fmt.Errorf("delegated cgroup is missing required controllers: %s", strings.Join(missing, ", "))
	}
	controllers := append([]string(nil), required...)
	if available["io"] {
		controllers = append(controllers, "io")
	}
	main := filepath.Join(path, "main")
	if err := mkdirCgroup(main); err != nil {
		return nil, fmt.Errorf("create service cgroup leaf: %w", err)
	}
	if err := writeCgroupValue(main, "cgroup.procs", strconv.Itoa(servicePID)); err != nil {
		return nil, fmt.Errorf("move service into its cgroup leaf: %w", err)
	}
	if err := enableCgroupControllers(path, controllers); err != nil {
		return nil, fmt.Errorf("enable delegated cgroup controllers: %w", err)
	}
	sessions := filepath.Join(path, "sessions")
	if err := mkdirCgroup(sessions); err != nil {
		return nil, fmt.Errorf("create sessions cgroup: %w", err)
	}
	if err := enableCgroupControllers(sessions, controllers); err != nil {
		return nil, fmt.Errorf("enable session cgroup controllers: %w", err)
	}
	return &CgroupRoot{path: sessions, controllers: controllers}, nil
}

func validateServiceCgroupProcesses(path string, servicePID int) error {
	contents, err := os.ReadFile(filepath.Join(path, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("read delegated cgroup processes: %w", err)
	}
	foundService := false
	for _, field := range strings.Fields(string(contents)) {
		pid, parseErr := strconv.Atoi(field)
		if parseErr != nil {
			return fmt.Errorf("invalid PID %q in delegated cgroup.procs", field)
		}
		if pid != servicePID {
			return fmt.Errorf("delegated cgroup must contain only service PID %d before controller setup (found PID %d)", servicePID, pid)
		}
		foundService = true
	}
	if !foundService {
		return fmt.Errorf("service PID %d is not in the delegated cgroup", servicePID)
	}
	return nil
}

// Create creates a limited cgroup below the delegated sessions hierarchy.
func (r *CgroupRoot) Create(id string, limits runtime.Limits) (*SessionCgroup, error) {
	return CreateSessionCgroup(r.path, id, limits)
}

// SessionCgroup manages limits and counters for one native sandbox.
type SessionCgroup struct {
	path string
}

// CreateSessionCgroup creates and configures one session's cgroup directory.
func CreateSessionCgroup(parent, id string, limits runtime.Limits) (*SessionCgroup, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("invalid session cgroup id %q", id)
	}
	path := filepath.Join(parent, id)
	if err := os.Mkdir(path, 0750); err != nil {
		return nil, fmt.Errorf("create session cgroup %s: %w", id, err)
	}
	cgroup := &SessionCgroup{path: path}
	if err := cgroup.UpdateLimits(limits); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return cgroup, nil
}

// UpdateLimits writes the configured CPU, memory, swap, and process ceilings.
func (c *SessionCgroup) UpdateLimits(limits runtime.Limits) error {
	if limits.CPUMillicores <= 0 || limits.MemoryMB <= 0 || limits.PIDs <= 0 {
		return fmt.Errorf("CPU, memory, and PID limits must be positive")
	}
	maxInt := int(^uint(0) >> 1)
	maxInt64 := uint64(^uint64(0) >> 1)
	if uint64(limits.MemoryMB) > maxInt64>>20 || limits.CPUMillicores > maxInt/100 {
		return fmt.Errorf("CPU or memory limit overflows cgroup v2 settings")
	}
	memoryBytes := int64(limits.MemoryMB) << 20
	memoryHigh := memoryBytes/10*9 + memoryBytes%10*9/10
	settings := []struct{ file, value string }{
		{"memory.max", strconv.FormatInt(memoryBytes, 10)},
		{"memory.swap.max", "0"},
		{"memory.oom.group", "1"},
		{"memory.high", strconv.FormatInt(memoryHigh, 10)},
		{"pids.max", strconv.Itoa(limits.PIDs)},
		{"cpu.max", fmt.Sprintf("%d %d", limits.CPUMillicores*100, cgroupPeriod)},
		{"cpu.weight", "100"},
	}
	for _, setting := range settings {
		if err := writeCgroupValue(c.path, setting.file, setting.value); err != nil {
			return fmt.Errorf("write cgroup %s: %w", setting.file, err)
		}
	}
	return nil
}

// Freeze pauses every task in the session cgroup before destructive quota cleanup.
func (c *SessionCgroup) Freeze() error {
	if err := writeCgroupValue(c.path, "cgroup.freeze", "1"); err != nil {
		return fmt.Errorf("freeze session cgroup: %w", err)
	}
	return nil
}

// Stats returns the current CPU, memory, and process counts from cgroup v2.
func (c *SessionCgroup) Stats() (runtime.Stats, error) {
	var stats runtime.Stats
	memory, err := readCgroupUint(c.path, "memory.current")
	if err != nil {
		return stats, err
	}
	stats.MemoryBytes = memory
	pids, err := readCgroupUint(c.path, "pids.current")
	if err != nil {
		return stats, err
	}
	stats.PIDs = int(pids)
	file, err := os.Open(filepath.Join(c.path, "cpu.stat"))
	if err != nil {
		return stats, fmt.Errorf("open cgroup cpu.stat: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	foundCPU := false
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "usage_usec" {
			usage, parseErr := strconv.ParseUint(fields[1], 10, 64)
			if parseErr != nil {
				return stats, fmt.Errorf("parse cgroup CPU usage: %w", parseErr)
			}
			stats.CPUUsageNS = usage * 1000
			foundCPU = true
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("read cgroup cpu.stat: %w", err)
	}
	if !foundCPU {
		return stats, errors.New("cgroup cpu.stat has no usage_usec counter")
	}
	ioStats, err := os.ReadFile(filepath.Join(c.path, "io.stat"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return stats, fmt.Errorf("read cgroup io.stat: %w", err)
	}
	if err == nil {
		for _, line := range strings.Split(string(ioStats), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			for _, field := range fields[1:] {
				key, value, ok := strings.Cut(field, "=")
				if !ok || (key != "rbytes" && key != "wbytes") {
					continue
				}
				count, parseErr := strconv.ParseUint(value, 10, 64)
				if parseErr != nil {
					return stats, fmt.Errorf("parse cgroup io.stat %s: %w", key, parseErr)
				}
				if key == "rbytes" {
					stats.IOReadBytes += count
				} else {
					stats.IOWriteBytes += count
				}
			}
		}
	}
	// memory.events is available on cgroup v2 and reports cumulative events.
	events, err := os.ReadFile(filepath.Join(c.path, "memory.events"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return stats, fmt.Errorf("read cgroup memory.events: %w", err)
	}
	if err == nil {
		for _, line := range strings.Split(string(events), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[0] != "oom_kill" {
				continue
			}
			stats.OOMKills, err = strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return stats, fmt.Errorf("parse cgroup memory.events oom_kill: %w", err)
			}
			break
		}
	}
	pidsEvents, err := os.ReadFile(filepath.Join(c.path, "pids.events"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return stats, fmt.Errorf("read cgroup pids.events: %w", err)
	}
	if err == nil {
		for _, line := range strings.Split(string(pidsEvents), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[0] != "max" {
				continue
			}
			stats.PIDsMaxEvents, err = strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return stats, fmt.Errorf("parse cgroup pids.events max: %w", err)
			}
			break
		}
	}
	return stats, nil
}

// Kill terminates every process in the session cgroup.
func (c *SessionCgroup) Kill() error {
	if err := writeCgroupValue(c.path, "cgroup.kill", "1"); err != nil {
		return fmt.Errorf("kill cgroup processes: %w", err)
	}
	return nil
}

// Destroy removes the cgroup after its processes and children have exited.
func (c *SessionCgroup) Destroy() error {
	if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove session cgroup: %w", err)
	}
	return nil
}

// WaitEmpty waits until cgroup v2 reports that no task remains in the session.
func (c *SessionCgroup) WaitEmpty(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		contents, err := os.ReadFile(filepath.Join(c.path, "cgroup.events"))
		if err != nil {
			return fmt.Errorf("read session cgroup events: %w", err)
		}
		found := false
		for _, line := range strings.Split(string(contents), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "populated" {
				found = true
				if fields[1] == "0" {
					return nil
				}
			}
		}
		if !found {
			return errors.New("session cgroup events has no populated field")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func writeCgroupValue(dir, file, value string) error {
	return os.WriteFile(filepath.Join(dir, file), []byte(value), 0600)
}

func readCgroupUint(dir, file string) (uint64, error) {
	contents, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return 0, fmt.Errorf("read cgroup %s: %w", file, err)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cgroup %s: %w", file, err)
	}
	return value, nil
}

func cgroupControllers(path string) (map[string]bool, error) {
	contents, err := os.ReadFile(filepath.Join(path, "cgroup.controllers"))
	if err != nil {
		return nil, err
	}
	controllers := make(map[string]bool)
	for _, name := range strings.Fields(string(contents)) {
		controllers[name] = true
	}
	return controllers, nil
}

func missingCgroupControllers(have map[string]bool, required []string) []string {
	var missing []string
	for _, name := range required {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func mkdirCgroup(path string) error {
	if err := os.Mkdir(path, 0750); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func enableCgroupControllers(path string, controllers []string) error {
	commands := make([]string, 0, len(controllers))
	for _, name := range controllers {
		commands = append(commands, "+"+name)
	}
	return writeCgroupValue(path, "cgroup.subtree_control", strings.Join(commands, " "))
}
