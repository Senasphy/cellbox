package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
	"golang.org/x/sys/unix"
)

type committedResources struct {
	cpuMilli int64
	memoryMB int64
	pids     int64
}

func activeSession(state string) bool {
	return state == "creating" || state == "ready" || state == "stopping"
}

func sessionResources(item store.SessionRecord) runtime.Limits {
	var limits runtime.Limits
	_ = json.Unmarshal(item.LimitsJSON, &limits)
	return limits
}

func (s *server) enforceKeyQuota(w http.ResponseWriter, r *http.Request, key store.KeyRecord, requested runtime.Limits) bool {
	var quota store.KeyQuotas
	_ = json.Unmarshal(key.QuotasJSON, &quota)
	if quota.MaxSessions == 0 && quota.MaxTotalMemoryMB == 0 {
		return true
	}
	sessions, err := s.listSessionsForAdmission(r.Context())
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "quota_check_failed"})
		return false
	}
	count, memory := 0, int64(0)
	for _, item := range sessions {
		if item.OwnerKeyID != key.ID || !activeSession(item.State) {
			continue
		}
		count++
		memory += int64(sessionResources(item).MemoryMB)
	}
	if quota.MaxSessions > 0 && count+1 > quota.MaxSessions || quota.MaxTotalMemoryMB > 0 && memory+int64(requested.MemoryMB) > int64(quota.MaxTotalMemoryMB) {
		problem.Write(w, problem.Problem{Title: "Quota exceeded", Status: http.StatusTooManyRequests, Code: "quota_exceeded", Detail: "API key session or memory quota would be exceeded"})
		return false
	}
	return true
}

func (s *server) enforceCapacity(w http.ResponseWriter, r *http.Request, requested runtime.Limits) bool {
	freeBytes, err := freeDiskBytes(s.cfg.DataDir)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "capacity_check_failed", Detail: "free disk space could not be determined"})
		return false
	}
	minimumBytes := uint64(s.cfg.Capacity.MinFreeDiskMB) * 1024 * 1024
	if freeBytes < minimumBytes {
		retryAfter := s.cfg.Capacity.QueueWaitS
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		problem.Write(w, problem.Problem{Title: "Disk capacity exceeded", Status: http.StatusServiceUnavailable, Code: "disk_capacity_exceeded", Detail: "host free space is below the configured admission floor"})
		return false
	}
	sessions, err := s.listSessionsForAdmission(r.Context())
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "capacity_check_failed"})
		return false
	}
	committed := committedResources{}
	for _, item := range sessions {
		if !activeSession(item.State) {
			continue
		}
		limits := sessionResources(item)
		committed.cpuMilli += int64(limits.CPUMillicores)
		committed.memoryMB += int64(limits.MemoryMB)
		committed.pids += int64(limits.PIDs)
	}
	host, err := readHostCapacity()
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "capacity_check_failed", Detail: "host capacity could not be determined"})
		return false
	}
	memLimit := float64(host.memoryMB) * s.cfg.Capacity.MemoryOvercommit
	cpuLimit := float64(host.cpuMilli) * s.cfg.Capacity.CPUOvercommit
	poolMemoryMB := int64(0)
	if s.pool != nil {
		poolMemoryMB = int64(s.pool.PoolMemoryMB())
	}
	if float64(committed.memoryMB+int64(requested.MemoryMB)+poolMemoryMB) > memLimit || float64(committed.cpuMilli+int64(requested.CPUMillicores)) > cpuLimit || committed.pids+int64(requested.PIDs) > host.pids {
		retryAfter := s.cfg.Capacity.QueueWaitS
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		problem.Write(w, problem.Problem{Title: "Capacity exceeded", Status: http.StatusServiceUnavailable, Code: "capacity_exceeded", Detail: "host capacity is fully committed"})
		return false
	}
	return true
}

func freeDiskBytes(path string) (uint64, error) {
	if path == "" {
		path = "."
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("resolve data directory for disk capacity: %w", err)
	}
	for {
		info, statErr := os.Stat(absolute)
		if statErr == nil && info.IsDir() {
			var disk unix.Statfs_t
			if err := unix.Statfs(absolute, &disk); err != nil {
				return 0, err
			}
			if disk.Bsize <= 0 {
				return 0, errors.New("filesystem reported an invalid block size")
			}
			blocks, blockSize := uint64(disk.Bavail), uint64(disk.Bsize)
			if blocks > ^uint64(0)/blockSize {
				return ^uint64(0), nil
			}
			return blocks * blockSize, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return 0, statErr
		}
		parent := filepath.Dir(absolute)
		if parent == absolute {
			if statErr != nil {
				return 0, statErr
			}
			return 0, errors.New("data directory has no existing filesystem ancestor")
		}
		absolute = parent
	}
}

func (s *server) listSessionsForAdmission(ctx context.Context) ([]store.SessionRecord, error) {
	persist, ok := s.store.(interface {
		ListSessions(context.Context, string, bool) ([]store.SessionRecord, error)
	})
	if !ok {
		return nil, fmt.Errorf("session listing unavailable")
	}
	return persist.ListSessions(ctx, "", true)
}

type hostCapacity struct {
	memoryMB int64
	cpuMilli int64
	pids     int64
}

func readHostCapacity() (hostCapacity, error) {
	var result hostCapacity
	result.cpuMilli = int64(goruntime.NumCPU()) * 1000
	result.pids = 32768
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				if kb, parseErr := strconv.ParseInt(fields[1], 10, 64); parseErr == nil {
					result.memoryMB = kb / 1024
				}
			}
		}
	}
	if result.memoryMB == 0 {
		return hostCapacity{}, fmt.Errorf("memory capacity unavailable")
	}
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		if limit, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); parseErr == nil && limit > 0 {
			limitMB := limit / (1024 * 1024)
			if limitMB < result.memoryMB {
				result.memoryMB = limitMB
			}
		}
	}
	if data, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) == 2 && fields[0] != "max" {
			quota, qErr := strconv.ParseInt(fields[0], 10, 64)
			period, pErr := strconv.ParseInt(fields[1], 10, 64)
			if qErr == nil && pErr == nil && period > 0 {
				cpu := quota * 1000 / period
				if cpu > 0 && cpu < result.cpuMilli {
					result.cpuMilli = cpu
				}
			}
		}
	}
	if data, err := os.ReadFile("/sys/fs/cgroup/pids.max"); err == nil {
		if limit, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); parseErr == nil && limit > 0 && limit < result.pids {
			result.pids = limit
		}
	}
	return result, nil
}
