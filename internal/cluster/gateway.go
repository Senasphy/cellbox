package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/runtime"
)

// Gateway routes cluster HTTP, SSE, and WebSocket requests to owning workers.
type Gateway struct {
	registry   *Registry
	fallback   http.Handler
	adminCheck func(*http.Request) bool
	proxyMu    sync.RWMutex
	proxies    map[string]*httputil.ReverseProxy
}

// NewGateway constructs a cluster HTTP gateway backed by registry and an optional
// local fallback handler for non-session control-plane routes.
func NewGateway(registry *Registry, fallback http.Handler, adminCheck func(*http.Request) bool) *Gateway {
	return &Gateway{
		registry:   registry,
		fallback:   fallback,
		adminCheck: adminCheck,
		proxies:    make(map[string]*httputil.ReverseProxy),
	}
}

// ServeHTTP dispatches cluster management, session placement, and session-scoped proxying.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/v1/cluster/workers" && r.Method == http.MethodGet {
		g.handleListWorkers(w, r)
		return
	}
	if strings.HasPrefix(path, "/v1/cluster/workers/") && strings.HasSuffix(path, "/drain") && r.Method == http.MethodPost {
		workerID := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/cluster/workers/"), "/drain")
		if workerID != "" && !strings.Contains(workerID, "/") {
			g.handleDrainWorker(w, r, workerID)
			return
		}
	}
	if path == "/v1/sessions" && r.Method == http.MethodPost {
		g.handleCreateSession(w, r)
		return
	}
	if sessionID, rest, ok := parseSessionPath(path); ok {
		if rest == "/fork" && r.Method == http.MethodPost {
			g.handleForkSession(w, r, sessionID)
			return
		}
		g.handleSessionProxy(w, r, sessionID)
		return
	}
	if g.fallback != nil {
		g.fallback.ServeHTTP(w, r)
		return
	}
	problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "not_found"})
}

func (g *Gateway) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	if g.adminCheck != nil && !g.adminCheck(r) {
		problem.Write(w, problem.Problem{Title: "Forbidden", Status: http.StatusForbidden, Code: "insufficient_scope"})
		return
	}
	workers := g.registry.List()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": workers})
}

func (g *Gateway) handleDrainWorker(w http.ResponseWriter, r *http.Request, workerID string) {
	if g.adminCheck != nil && !g.adminCheck(r) {
		problem.Write(w, problem.Problem{Title: "Forbidden", Status: http.StatusForbidden, Code: "insufficient_scope"})
		return
	}
	if err := g.registry.Drain(r.Context(), workerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "worker_not_found"})
			return
		}
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "worker_drain_failed", Detail: err.Error()})
		return
	}
	info, _ := g.registry.Get(workerID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func (g *Gateway) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	body, limits, err := readCreateLimits(r)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
		return
	}
	worker, err := g.registry.SelectWorker(limits)
	if err != nil {
		w.Header().Set("Retry-After", "5")
		problem.Write(w, problem.Problem{Title: "Capacity exceeded", Status: http.StatusServiceUnavailable, Code: "capacity_exceeded", Detail: "no ready cluster worker has capacity"})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }

	g.proxyCreateAndRecord(w, r, worker)
}

func (g *Gateway) handleForkSession(w http.ResponseWriter, r *http.Request, sourceSessionID string) {
	body, limits, err := readCreateLimits(r)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
		return
	}
	// Prefer the worker that owns the source session if it is ready, otherwise schedule on any ready worker.
	worker, ok, lost := g.registry.SessionWorker(r.Context(), sourceSessionID)
	if lost || !ok || worker.State != WorkerReady {
		worker, err = g.registry.SelectWorker(limits)
		if err != nil {
			w.Header().Set("Retry-After", "5")
			problem.Write(w, problem.Problem{Title: "Capacity exceeded", Status: http.StatusServiceUnavailable, Code: "capacity_exceeded", Detail: "no ready cluster worker has capacity"})
			return
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }

	g.proxyCreateAndRecord(w, r, worker)
}

func (g *Gateway) proxyCreateAndRecord(w http.ResponseWriter, r *http.Request, worker WorkerInfo) {
	rec := &statusCaptureWriter{header: make(http.Header)}
	proxy := g.proxyFor(worker.AdvertiseURL)
	if proxy == nil {
		problem.Write(w, problem.Problem{Title: "Worker unavailable", Status: http.StatusServiceUnavailable, Code: "worker_unavailable"})
		return
	}
	proxy.ServeHTTP(rec, r)
	if rec.status == http.StatusCreated {
		var created struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(rec.body.Bytes(), &created) == nil && created.ID != "" {
			_ = g.registry.RecordSessionPlacement(r.Context(), created.ID, worker.ID)
		}
	}
	for k, vals := range rec.header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body.Bytes())
}

func (g *Gateway) handleSessionProxy(w http.ResponseWriter, r *http.Request, sessionID string) {
	worker, ok, lost := g.registry.SessionWorker(r.Context(), sessionID)
	if lost {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/"+sessionID && g.fallback != nil {
			g.fallback.ServeHTTP(w, r)
			return
		}
		problem.Write(w, problem.Problem{Title: "Worker lost", Status: http.StatusServiceUnavailable, Code: "worker_lost", Detail: "the worker hosting this session is lost"})
		return
	}
	if !ok || worker.AdvertiseURL == "" {
		if g.fallback != nil {
			g.fallback.ServeHTTP(w, r)
			return
		}
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "session_not_found"})
		return
	}
	proxy := g.proxyFor(worker.AdvertiseURL)
	if proxy == nil {
		problem.Write(w, problem.Problem{Title: "Worker unavailable", Status: http.StatusServiceUnavailable, Code: "worker_unavailable"})
		return
	}
	proxy.ServeHTTP(w, r)
}

func (g *Gateway) proxyFor(targetURL string) *httputil.ReverseProxy {
	g.proxyMu.RLock()
	p, ok := g.proxies[targetURL]
	g.proxyMu.RUnlock()
	if ok {
		return p
	}
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Host == "" {
		return nil
	}
	proxy := httputil.NewSingleHostReverseProxy(parsed)
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}
		problem.Write(w, problem.Problem{Title: "Worker unavailable", Status: http.StatusServiceUnavailable, Code: "worker_unavailable", Detail: err.Error()})
	}
	g.proxyMu.Lock()
	g.proxies[targetURL] = proxy
	g.proxyMu.Unlock()
	return proxy
}

func parseSessionPath(path string) (sessionID, rest string, ok bool) {
	const prefix = "/v1/sessions/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	tail := strings.TrimPrefix(path, prefix)
	if tail == "" {
		return "", "", false
	}
	if idx := strings.IndexByte(tail, '/'); idx >= 0 {
		return tail[:idx], tail[idx:], true
	}
	return tail, "", true
}

func readCreateLimits(r *http.Request) ([]byte, runtime.Limits, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, runtime.Limits{}, err
	}
	var payload struct {
		Limits *runtime.Limits `json:"limits"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload.Limits != nil {
		return body, *payload.Limits, nil
	}
	return body, runtime.Limits{CPUMillicores: 1000, MemoryMB: 512, PIDs: 256}, nil
}

type statusCaptureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *statusCaptureWriter) Header() http.Header { return w.header }
func (w *statusCaptureWriter) WriteHeader(code int) {
	w.status = code
	w.header.Del("Content-Length")
}
func (w *statusCaptureWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *statusCaptureWriter) Flush() {}

func parseRetryAfter(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
