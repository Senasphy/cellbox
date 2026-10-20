package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

// KeyStore provides authentication lookups and readiness checks.
type KeyStore interface {
	Ping(context.Context) error
	FindKeyByPrefix(context.Context, string) (store.KeyRecord, error)
}

// System reports non-secret service capabilities.
type System struct {
	Backend      string               `json:"backend"`
	Capabilities runtime.Capabilities `json:"capabilities"`
}

type server struct {
	store       KeyStore
	backend     runtime.Runtime
	backendName string
	mux         *http.ServeMux
	cfg         config.Config
	execMu      sync.RWMutex
	activeExecs map[string]runtime.ExecHandle
	idemMu      sync.Mutex
	idemLocks   map[string]*idemLock
	rateMu      sync.Mutex
	rateCounts  map[string]rateWindow
}

// NewHandler builds the initial HTTP handler with health and authenticated system routes.
func NewHandler(store KeyStore, backend runtime.Runtime, backendName string) http.Handler {
	return NewHandlerWithConfig(store, backend, backendName, config.Defaults())
}

// NewHandlerWithConfig builds the service routes with configured session limits.
func NewHandlerWithConfig(store KeyStore, backend runtime.Runtime, backendName string, cfg config.Config) http.Handler {
	s := &server{store: store, backend: backend, backendName: backendName, mux: http.NewServeMux(), cfg: cfg, activeExecs: map[string]runtime.ExecHandle{}, idemLocks: map[string]*idemLock{}, rateCounts: map[string]rateWindow{}}
	s.mux.Handle("GET /healthz", withTimeout(5*time.Second, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})))
	s.mux.Handle("GET /readyz", withTimeout(5*time.Second, http.HandlerFunc(s.ready)))
	s.mux.Handle("GET /metrics", withTimeout(10*time.Second, http.HandlerFunc(s.metrics)))
	s.mux.Handle("GET /v1/system", withTimeout(5*time.Second, http.HandlerFunc(s.system)))
	s.mux.Handle("POST /v1/keys", withTimeout(10*time.Second, http.HandlerFunc(s.createKey)))
	s.mux.Handle("GET /v1/keys", withTimeout(10*time.Second, http.HandlerFunc(s.listKeys)))
	s.mux.Handle("DELETE /v1/keys/{id}", withTimeout(10*time.Second, http.HandlerFunc(s.deleteKey)))
	s.mux.Handle("POST /v1/sessions", withTimeout(15*time.Minute, http.HandlerFunc(s.createSession)))
	s.mux.Handle("GET /v1/sessions", withTimeout(10*time.Second, http.HandlerFunc(s.listSessions)))
	s.mux.HandleFunc("GET /v1/sessions/stream", s.streamSessions)
	s.mux.Handle("GET /v1/sessions/{id}", withTimeout(10*time.Second, http.HandlerFunc(s.getSession)))
	s.mux.Handle("GET /v1/sessions/{id}/metrics", withTimeout(10*time.Second, http.HandlerFunc(s.sessionMetrics)))
	s.mux.HandleFunc("GET /v1/sessions/{id}/metrics/stream", s.streamSessionMetrics)
	s.mux.Handle("GET /v1/sessions/{id}/events", withTimeout(10*time.Second, http.HandlerFunc(s.listSessionEvents)))
	s.mux.Handle("DELETE /v1/sessions/{id}", withTimeout(2*time.Minute, http.HandlerFunc(s.deleteSession)))
	s.mux.Handle("POST /v1/sessions/{id}/execs", withTimeout(time.Duration(sessionLimitMaxTimeout(cfg.Limits.Max.TTLS)+30_000)*time.Millisecond, http.HandlerFunc(s.createExec)))
	s.mux.Handle("GET /v1/sessions/{id}/execs", withTimeout(10*time.Second, http.HandlerFunc(s.listExecs)))
	s.mux.Handle("GET /v1/sessions/{id}/execs/{execID}", withTimeout(10*time.Second, http.HandlerFunc(s.getExec)))
	s.mux.Handle("POST /v1/sessions/{id}/execs/{execID}/signal", withTimeout(10*time.Second, http.HandlerFunc(s.signalExec)))
	s.mux.HandleFunc("GET /v1/sessions/{id}/execs/{execID}/stream", s.streamExec)
	s.mux.Handle("GET /v1/sessions/{id}/files", withTimeout(10*time.Minute, http.HandlerFunc(s.readFile)))
	s.mux.Handle("PUT /v1/sessions/{id}/files", withTimeout(10*time.Minute, http.HandlerFunc(s.writeFile)))
	s.mux.Handle("DELETE /v1/sessions/{id}/files", withTimeout(2*time.Minute, http.HandlerFunc(s.deleteFile)))
	s.mux.Handle("GET /v1/sessions/{id}/files/list", withTimeout(10*time.Minute, http.HandlerFunc(s.listFiles)))
	s.mux.Handle("GET /v1/sessions/{id}/archive", withTimeout(20*time.Minute, http.HandlerFunc(s.downloadArchive)))
	s.mux.Handle("PUT /v1/sessions/{id}/archive", withTimeout(20*time.Minute, http.HandlerFunc(s.uploadArchive)))
	return s.idempotencyMiddleware(s.mux)
}

func withTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	if s.store != nil {
		if err := s.store.Ping(r.Context()); err != nil {
			problem.Write(w, problem.Problem{Title: "Not ready", Status: http.StatusServiceUnavailable, Code: "not_ready", Detail: "persistent store is unavailable"})
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
func (s *server) system(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if key.Scope != "admin" {
		problem.Write(w, problem.Problem{Title: "Forbidden", Status: http.StatusForbidden, Code: "insufficient_scope"})
		return
	}
	caps := runtime.Capabilities{}
	if s.backend != nil {
		caps = s.backend.Capabilities()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(System{Backend: s.backendName, Capabilities: caps})
}
func (s *server) authenticate(w http.ResponseWriter, r *http.Request) (store.KeyRecord, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		problem.Write(w, problem.Problem{Title: "Unauthorized", Status: http.StatusUnauthorized, Code: "unauthorized"})
		return store.KeyRecord{}, false
	}
	prefix, _, err := auth.Parse(parts[1])
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Unauthorized", Status: http.StatusUnauthorized, Code: "unauthorized"})
		return store.KeyRecord{}, false
	}
	if s.store == nil {
		problem.Write(w, problem.Problem{Title: "Unauthorized", Status: http.StatusUnauthorized, Code: "unauthorized"})
		return store.KeyRecord{}, false
	}
	record, err := s.store.FindKeyByPrefix(r.Context(), prefix)
	if err != nil || !auth.Verify(parts[1], record.Hash) {
		problem.Write(w, problem.Problem{Title: "Unauthorized", Status: http.StatusUnauthorized, Code: "unauthorized"})
		return store.KeyRecord{}, false
	}
	if !s.allowKeyRequest(w, record) {
		return store.KeyRecord{}, false
	}
	if updater, ok := s.store.(interface {
		MarkKeyUsed(context.Context, string, time.Time) error
	}); ok {
		_ = updater.MarkKeyUsed(r.Context(), record.ID, time.Now().UTC())
	}
	return record, true
}

type rateWindow struct {
	requests []time.Time
}

func (s *server) allowKeyRequest(w http.ResponseWriter, record store.KeyRecord) bool {
	var quotas store.KeyQuotas
	_ = json.Unmarshal(record.QuotasJSON, &quotas)
	if quotas.RequestsPerSecond <= 0 {
		return true
	}
	now := time.Now()
	s.rateMu.Lock()
	window := s.rateCounts[record.ID]
	cutoff := now.Add(-time.Second)
	first := 0
	for first < len(window.requests) && !window.requests[first].After(cutoff) {
		first++
	}
	window.requests = append(window.requests[:0], window.requests[first:]...)
	allowed := len(window.requests) < quotas.RequestsPerSecond
	if allowed {
		window.requests = append(window.requests, now)
	}
	s.rateCounts[record.ID] = window
	s.rateMu.Unlock()
	if allowed {
		return true
	}
	w.Header().Set("Retry-After", "1")
	problem.Write(w, problem.Problem{Title: "Rate limit exceeded", Status: http.StatusTooManyRequests, Code: "rate_limited", Detail: "API key request rate quota exceeded"})
	return false
}
