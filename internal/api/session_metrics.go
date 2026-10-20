package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

type sessionMetricsSample struct {
	SampledAt    time.Time      `json:"sampled_at"`
	CPUUsageNS   uint64         `json:"cpu_usage_ns"`
	MemoryBytes  uint64         `json:"memory_bytes"`
	PIDs         int            `json:"pids"`
	IOReadBytes  uint64         `json:"io_read_bytes"`
	IOWriteBytes uint64         `json:"io_write_bytes"`
	Limits       runtime.Limits `json:"limits"`
}

func (s *server) sessionMetrics(w http.ResponseWriter, r *http.Request) {
	_, record, box, ok := s.sessionMetricsTarget(w, r)
	if !ok {
		return
	}
	sample, err := readSessionMetrics(r.Context(), record, box)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Metrics unavailable", Status: http.StatusBadGateway, Code: "session_metrics_unavailable", Detail: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sample)
}

func (s *server) streamSessionMetrics(w http.ResponseWriter, r *http.Request) {
	_, record, box, ok := s.sessionMetricsTarget(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Streaming unavailable", Status: http.StatusInternalServerError, Code: "stream_unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		sample, err := readSessionMetrics(r.Context(), record, box)
		if err != nil {
			payload, _ := json.Marshal(map[string]string{"code": "session_metrics_unavailable", "detail": err.Error()})
			_, _ = fmt.Fprintf(w, "event: problem\ndata: %s\n\n", payload)
			flusher.Flush()
			return
		}
		payload, err := json.Marshal(sample)
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *server) sessionMetricsTarget(w http.ResponseWriter, r *http.Request) (store.KeyRecord, store.SessionRecord, runtime.Sandbox, bool) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return store.KeyRecord{}, store.SessionRecord{}, nil, false
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return store.KeyRecord{}, store.SessionRecord{}, nil, false
	}
	record, err := persist.GetSession(r.Context(), r.PathValue("id"))
	if err != nil || !canSee(key, record) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "session_not_found"})
		return store.KeyRecord{}, store.SessionRecord{}, nil, false
	}
	resolver, ok := s.backend.(sandboxResolver)
	if !ok || record.RuntimeID == "" {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable", Detail: "the runtime cannot reconnect to this session"})
		return store.KeyRecord{}, store.SessionRecord{}, nil, false
	}
	box := resolver.SandboxByID(record.RuntimeID)
	if box == nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable", Detail: "the runtime cannot reconnect to this session"})
		return store.KeyRecord{}, store.SessionRecord{}, nil, false
	}
	return key, record, box, true
}

func readSessionMetrics(ctx context.Context, record store.SessionRecord, box runtime.Sandbox) (sessionMetricsSample, error) {
	stats, err := box.Stats(ctx)
	if err != nil {
		return sessionMetricsSample{}, err
	}
	return sessionMetricsSample{
		SampledAt: time.Now().UTC(), CPUUsageNS: stats.CPUUsageNS, MemoryBytes: stats.MemoryBytes,
		PIDs: stats.PIDs, IOReadBytes: stats.IOReadBytes, IOWriteBytes: stats.IOWriteBytes,
		Limits: sessionResources(record),
	}, nil
}
