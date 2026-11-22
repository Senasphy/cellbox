package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/store"
)

type metricsStore interface {
	ListSessions(context.Context, string, bool) ([]store.SessionRecord, error)
	ListExecs(context.Context, string) ([]store.ExecRecord, error)
	ListAuditEvents(context.Context, string, int) ([]store.AuditEventRecord, error)
}

func (s *server) metrics(w http.ResponseWriter, r *http.Request) {
	persist, ok := s.store.(metricsStore)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "metrics_unavailable"})
		return
	}
	sessions, err := persist.ListSessions(r.Context(), "", true)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "metrics_unavailable"})
		return
	}
	states := map[string]int{"creating": 0, "ready": 0, "stopping": 0, "stopped": 0, "expired": 0, "failed": 0, "lost": 0}
	committed := committedResources{}
	var sessionErrors uint64
	var outputBytes uint64
	var execCount uint64
	var execTime time.Duration
	var buckets = []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 30, 60}
	bucketCounts := make([]uint64, len(buckets))
	results := map[string]uint64{"ok": 0, "nonzero": 0, "timed_out": 0, "oom_killed": 0, "pids_limited": 0, "failed": 0}
	breaches := map[string]uint64{"memory": 0, "pids": 0, "disk": 0, "output": 0, "timeout": 0}
	createDurations := map[string][]float64{"cold": {}, "warm": {}}
	for _, session := range sessions {
		if _, exists := states[session.State]; exists {
			states[session.State]++
		}
		if session.State == "failed" || session.State == "lost" {
			sessionErrors++
		}
		if activeSession(session.State) {
			limits := sessionResources(session)
			committed.cpuMilli += int64(limits.CPUMillicores)
			committed.memoryMB += int64(limits.MemoryMB)
			committed.pids += int64(limits.PIDs)
		}
		events, auditErr := persist.ListAuditEvents(r.Context(), session.ID, 1000)
		if auditErr != nil {
			problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "metrics_unavailable"})
			return
		}
		var createdAt, readyAt time.Time
		readySource := "cold"
		for _, event := range events {
			if event.Type == "limit.breach" {
				var detail struct {
					Kind string `json:"kind"`
				}
				if json.Unmarshal(event.Detail, &detail) == nil {
					if _, known := breaches[detail.Kind]; known {
						breaches[detail.Kind]++
					}
				}
			}
			switch event.Type {
			case "session.create":
				createdAt = event.Timestamp
			case "session.ready":
				readyAt = event.Timestamp
				var detail struct {
					Source string `json:"source"`
				}
				if json.Unmarshal(event.Detail, &detail) == nil && detail.Source == "warm" {
					readySource = "warm"
				}
			}
		}
		if !createdAt.IsZero() && !readyAt.IsZero() && !readyAt.Before(createdAt) {
			createDurations[readySource] = append(createDurations[readySource], readyAt.Sub(createdAt).Seconds())
		}
		execs, listErr := persist.ListExecs(r.Context(), session.ID)
		if listErr != nil {
			problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "metrics_unavailable"})
			return
		}
		for _, exec := range execs {
			outputBytes += uint64(exec.OutputBytes)
			if exec.State == "running" || exec.State == "starting" {
				continue
			}
			result := "failed"
			switch exec.State {
			case "timed_out":
				result = "timed_out"
			case "oom_killed":
				result = "oom_killed"
			case "pids_limited":
				result = "pids_limited"
			case "killed":
				result = "failed"
			case "exited":
				if exec.ExitCode != nil && *exec.ExitCode == 0 {
					result = "ok"
				} else {
					result = "nonzero"
				}
			}
			results[result]++
			if result == "failed" || result == "timed_out" || result == "oom_killed" || result == "pids_limited" {
				sessionErrors++
			}
			if exec.EndedAt != nil {
				execCount++
				seconds := exec.EndedAt.Sub(exec.StartedAt).Seconds()
				if seconds < 0 {
					seconds = 0
				}
				execTime += time.Duration(seconds * float64(time.Second))
				for i, bound := range buckets {
					if seconds <= bound {
						bucketCounts[i]++
					}
				}
			}
		}
	}
	host, err := readHostCapacity()
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "metrics_unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	f := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
	f("# HELP cellbox_sessions Current sessions by lifecycle state.\n# TYPE cellbox_sessions gauge\n")
	for _, state := range []string{"creating", "ready", "stopping", "stopped", "expired", "failed", "lost"} {
		f("cellbox_sessions{state=%q} %d\n", state, states[state])
	}
	f("# HELP cellbox_capacity_committed Resources reserved by active sessions.\n# TYPE cellbox_capacity_committed gauge\n")
	f("cellbox_capacity_committed{resource=\"cpu_millicores\"} %d\n", committed.cpuMilli)
	f("cellbox_capacity_committed{resource=\"memory_mb\"} %d\n", committed.memoryMB)
	if s.pool != nil {
		f("cellbox_capacity_committed{resource=\"pool_memory_mb\"} %d\n", s.pool.PoolMemoryMB())
	}
	f("cellbox_capacity_committed{resource=\"pids\"} %d\n", committed.pids)
	f("# HELP cellbox_capacity_total Effective resource capacity after configured overcommit.\n# TYPE cellbox_capacity_total gauge\n")
	f("cellbox_capacity_total{resource=\"cpu_millicores\"} %s\n", strconv.FormatFloat(float64(host.cpuMilli)*s.cfg.Capacity.CPUOvercommit, 'f', -1, 64))
	f("cellbox_capacity_total{resource=\"memory_mb\"} %s\n", strconv.FormatFloat(float64(host.memoryMB)*s.cfg.Capacity.MemoryOvercommit, 'f', -1, 64))
	f("cellbox_capacity_total{resource=\"pids\"} %d\n", host.pids)
	if s.pool != nil {
		f("# HELP cellbox_pool_ready Idle warm sandboxes available by image.\n# TYPE cellbox_pool_ready gauge\n")
		for image, count := range s.pool.PoolReady() {
			f("cellbox_pool_ready{image=%q} %d\n", image, count)
		}
	}
	f("# HELP cellbox_exec_total Completed exec count by result.\n# TYPE cellbox_exec_total counter\n")
	for _, result := range []string{"ok", "nonzero", "timed_out", "oom_killed", "pids_limited", "failed"} {
		f("cellbox_exec_total{result=%q} %d\n", result, results[result])
	}
	f("# HELP cellbox_limit_breach_total Recorded resource limit breaches by kind.\n# TYPE cellbox_limit_breach_total counter\n")
	for _, kind := range []string{"memory", "pids", "disk", "output", "timeout"} {
		f("cellbox_limit_breach_total{kind=%q} %d\n", kind, breaches[kind])
	}
	f("# HELP cellbox_session_create_seconds Session creation duration.\n# TYPE cellbox_session_create_seconds histogram\n")
	for _, source := range []string{"cold", "warm"} {
		values := createDurations[source]
		sum := 0.0
		for _, duration := range values {
			sum += duration
		}
		labels := fmt.Sprintf("source=%q", source)
		renderHistogram(f, "cellbox_session_create_seconds", labels, buckets, values)
		f("cellbox_session_create_seconds_sum{source=%q} %s\ncellbox_session_create_seconds_count{source=%q} %d\n", source, strconv.FormatFloat(sum, 'f', -1, 64), source, len(values))
	}
	f("# HELP cellbox_exec_seconds Duration of completed execs.\n# TYPE cellbox_exec_seconds histogram\n")
	for i, bound := range buckets {
		f("cellbox_exec_seconds_bucket{le=%q} %d\n", strconv.FormatFloat(bound, 'f', -1, 64), bucketCounts[i])
	}
	f("cellbox_exec_seconds_bucket{le=\"+Inf\"} %d\n", execCount)
	f("cellbox_exec_seconds_sum %s\ncellbox_exec_seconds_count %d\n", strconv.FormatFloat(execTime.Seconds(), 'f', -1, 64), execCount)
	f("# HELP cellbox_output_bytes_total Exec output bytes persisted.\n# TYPE cellbox_output_bytes_total counter\ncellbox_output_bytes_total %d\n", outputBytes)
	f("# HELP cellbox_session_create_total Persisted session creation attempts.\n# TYPE cellbox_session_create_total counter\ncellbox_session_create_total %d\n", len(sessions))
	f("# HELP cellbox_error_total Failed sessions and exec operations.\n# TYPE cellbox_error_total counter\ncellbox_error_total %d\n", sessionErrors)
}

func renderHistogram(write func(string, ...any), name, labels string, bounds []float64, values []float64) {
	for _, bound := range bounds {
		var count int
		for _, value := range values {
			if value <= bound {
				count++
			}
		}
		write(name+"_bucket{"+labels+",le=%q} %d\n", strconv.FormatFloat(bound, 'f', -1, 64), count)
	}
	write(name+"_bucket{"+labels+`,le="+Inf"} %d\n`, len(values))
}
