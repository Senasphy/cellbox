package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

type sessionSnapshot struct {
	ID          string            `json:"id"`
	Image       string            `json:"image"`
	State       string            `json:"state"`
	Limits      runtime.Limits    `json:"limits"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   time.Time         `json:"created_at"`
	SampledAt   *time.Time        `json:"sampled_at,omitempty"`
	CPUUsageNS  *uint64           `json:"cpu_usage_ns,omitempty"`
	MemoryBytes *uint64           `json:"memory_bytes,omitempty"`
}

type sessionResourceSample struct {
	SampledAt   time.Time
	CPUUsageNS  uint64
	MemoryBytes uint64
}

func (s *server) streamSessions(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.sessionStore(w)
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
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	metricsTicker := time.NewTicker(5 * time.Second)
	defer metricsTicker.Stop()
	latestMetrics := map[string]sessionResourceSample{}
	for {
		records, _, err := persist.PageSessions(r.Context(), key.ID, key.Scope == "admin", 200, nil)
		if err != nil {
			payload, _ := json.Marshal(map[string]string{"code": "session_list_failed"})
			_, _ = fmt.Fprintf(w, "event: problem\ndata: %s\n\n", payload)
			flusher.Flush()
			return
		}
		items := make([]sessionSnapshot, 0, len(records))
		for _, record := range records {
			response := toSessionResponse(record)
			item := sessionSnapshot{ID: response.ID, Image: response.Image, State: response.State, Limits: response.Limits, Labels: response.Labels, CreatedAt: response.CreatedAt}
			if sample, exists := latestMetrics[record.ID]; exists {
				sampledAt := sample.SampledAt
				cpuUsageNS, memoryBytes := sample.CPUUsageNS, sample.MemoryBytes
				item.SampledAt, item.CPUUsageNS, item.MemoryBytes = &sampledAt, &cpuUsageNS, &memoryBytes
			}
			items = append(items, item)
		}
		payload, err := json.Marshal(map[string]any{"items": items})
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "event: sessions\ndata: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-metricsTicker.C:
			latestMetrics = collectSessionResourceSamples(r.Context(), s.backend, records)
		}
	}
}

func collectSessionResourceSamples(ctx context.Context, backend runtime.Runtime, records []store.SessionRecord) map[string]sessionResourceSample {
	resolver, ok := backend.(sandboxResolver)
	if !ok {
		return nil
	}
	active := make([]store.SessionRecord, 0, len(records))
	for _, record := range records {
		if record.State == "ready" && record.RuntimeID != "" {
			active = append(active, record)
		}
	}
	if len(active) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	workers := 8
	if len(active) < workers {
		workers = len(active)
	}
	jobs := make(chan store.SessionRecord)
	results := make(chan struct {
		id     string
		sample sessionResourceSample
	}, len(active))
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for record := range jobs {
				box := resolver.SandboxByID(record.RuntimeID)
				if box == nil {
					continue
				}
				stats, err := box.Stats(ctx)
				if err != nil {
					continue
				}
				results <- struct {
					id     string
					sample sessionResourceSample
				}{id: record.ID, sample: sessionResourceSample{SampledAt: time.Now().UTC(), CPUUsageNS: stats.CPUUsageNS, MemoryBytes: stats.MemoryBytes}}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, record := range active {
			select {
			case jobs <- record:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wait.Wait()
		close(results)
	}()
	samples := make(map[string]sessionResourceSample, len(active))
	for result := range results {
		samples[result.id] = result.sample
	}
	return samples
}
