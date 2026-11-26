package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/doctor"
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
	Backend         string               `json:"backend"`
	Capabilities    runtime.Capabilities `json:"capabilities"`
	DoctorStatus    string               `json:"doctor_status"`
	DoctorChecks    []string             `json:"doctor_checks"`
	EffectiveConfig map[string]any       `json:"effective_config"`
}

type server struct {
	store            KeyStore
	backend          runtime.Runtime
	pool             warmSessionPool
	backendName      string
	mux              *http.ServeMux
	cfg              config.Config
	execMu           sync.RWMutex
	activeExecs      map[string]runtime.ExecHandle
	terminalMu       sync.Mutex
	terminals        map[string]*terminalHub
	idemMu           sync.Mutex
	idemLocks        map[string]*idemLock
	rateMu           sync.Mutex
	rateCounts       map[string]rateWindow
	doctorOnce       sync.Once
	doctorMu         sync.RWMutex
	doctorState      string
	doctorInfo       []string
	terminalUpgrader websocket.Upgrader
	httpInFlight     atomic.Int64
	httpMetricsMu    sync.Mutex
	httpRequests     map[httpMetricKey]uint64
	httpDurations    map[httpRouteKey]*httpDurationHist
}

type warmSessionPool interface {
	Claim(context.Context, runtime.SandboxSpec) (runtime.Sandbox, bool)
	Refill(runtime.SandboxSpec)
	PoolReady() map[string]int
	PoolMemoryMB() int
}

// NewHandler builds the initial HTTP handler with health and authenticated system routes.
func NewHandler(store KeyStore, backend runtime.Runtime, backendName string) http.Handler {
	return NewHandlerWithConfig(store, backend, backendName, config.Defaults())
}

// NewHandlerWithConfig builds the service routes with configured session limits.
func NewHandlerWithConfig(store KeyStore, backend runtime.Runtime, backendName string, cfg config.Config) http.Handler {
	return newHandlerWithConfig(store, backend, backendName, cfg, nil)
}

// NewHandlerWithPool enables warm claims without hiding optional runtime capabilities.
func NewHandlerWithPool(store KeyStore, backend runtime.Runtime, backendName string, cfg config.Config, warmPool warmSessionPool) http.Handler {
	return newHandlerWithConfig(store, backend, backendName, cfg, warmPool)
}

func newHandlerWithConfig(store KeyStore, backend runtime.Runtime, backendName string, cfg config.Config, warmPool warmSessionPool) http.Handler {
	s := &server{store: store, backend: backend, pool: warmPool, backendName: backendName, mux: http.NewServeMux(), cfg: cfg, activeExecs: map[string]runtime.ExecHandle{}, terminals: map[string]*terminalHub{}, idemLocks: map[string]*idemLock{}, rateCounts: map[string]rateWindow{}, terminalUpgrader: websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}, httpRequests: map[httpMetricKey]uint64{}, httpDurations: map[httpRouteKey]*httpDurationHist{}}
	s.mux.Handle("GET /healthz", withTimeout(5*time.Second, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})))
	s.mux.Handle("GET /readyz", withTimeout(5*time.Second, http.HandlerFunc(s.ready)))
	s.mux.Handle("GET /metrics", withTimeout(10*time.Second, http.HandlerFunc(s.metrics)))
	s.mux.Handle("GET /v1/system", withTimeout(5*time.Second, http.HandlerFunc(s.system)))
	s.mux.Handle("GET /v1/images", withTimeout(30*time.Second, http.HandlerFunc(s.listImages)))
	s.mux.Handle("POST /v1/images", withTimeout(15*time.Minute, http.HandlerFunc(s.pullImage)))
	s.mux.Handle("DELETE /v1/images", withTimeout(30*time.Second, http.HandlerFunc(s.removeImage)))
	s.mux.Handle("GET /v1/limit-breaches", withTimeout(10*time.Second, http.HandlerFunc(s.listRecentLimitBreaches)))
	s.mux.Handle("POST /v1/keys", withTimeout(10*time.Second, http.HandlerFunc(s.createKey)))
	s.mux.Handle("GET /v1/keys", withTimeout(10*time.Second, http.HandlerFunc(s.listKeys)))
	s.mux.Handle("DELETE /v1/keys/{id}", withTimeout(10*time.Second, http.HandlerFunc(s.deleteKey)))
	s.mux.Handle("POST /v1/sessions", withTimeout(15*time.Minute, http.HandlerFunc(s.createSession)))
	s.mux.Handle("GET /v1/sessions", withTimeout(10*time.Second, http.HandlerFunc(s.listSessions)))
	s.mux.HandleFunc("GET /v1/sessions/stream", s.streamSessions)
	s.mux.Handle("GET /v1/sessions/{id}", withTimeout(10*time.Second, http.HandlerFunc(s.getSession)))
	s.mux.Handle("GET /v1/sessions/{id}/metrics", withTimeout(10*time.Second, http.HandlerFunc(s.sessionMetrics)))
	s.mux.HandleFunc("GET /v1/sessions/{id}/metrics/stream", s.streamSessionMetrics)
	s.mux.HandleFunc("GET /v1/sessions/{id}/terminal", s.sessionTerminal)
	s.mux.Handle("GET /v1/sessions/{id}/events", withTimeout(10*time.Second, http.HandlerFunc(s.listSessionEvents)))
	s.mux.Handle("GET /v1/sessions/{id}/replay", withTimeout(30*time.Second, http.HandlerFunc(s.replaySession)))
	s.mux.Handle("GET /v1/sessions/{id}/egress-log", withTimeout(10*time.Second, http.HandlerFunc(s.listSessionEgressLog)))
	s.mux.Handle("POST /v1/sessions/{id}/snapshots", withTimeout(15*time.Minute, http.HandlerFunc(s.createSnapshot)))
	s.mux.Handle("GET /v1/sessions/{id}/snapshots", withTimeout(10*time.Second, http.HandlerFunc(s.listSessionSnapshots)))
	s.mux.Handle("POST /v1/sessions/{id}/fork", withTimeout(15*time.Minute, http.HandlerFunc(s.forkSession)))
	s.mux.Handle("GET /v1/snapshots/{snapshotID}", withTimeout(10*time.Second, http.HandlerFunc(s.getSnapshot)))
	s.mux.Handle("DELETE /v1/snapshots/{snapshotID}", withTimeout(2*time.Minute, http.HandlerFunc(s.deleteSnapshot)))
	s.mux.Handle("DELETE /v1/sessions/{id}", withTimeout(2*time.Minute, http.HandlerFunc(s.deleteSession)))
	s.mux.Handle("POST /v1/sessions/{id}/execs", withTimeout(time.Duration(sessionLimitMaxTimeout(cfg.Limits.Max.TTLS)+30_000)*time.Millisecond, http.HandlerFunc(s.createExec)))
	s.mux.Handle("GET /v1/sessions/{id}/execs", withTimeout(10*time.Second, http.HandlerFunc(s.listExecs)))
	s.mux.Handle("GET /v1/sessions/{id}/execs/{execID}", withTimeout(10*time.Second, http.HandlerFunc(s.getExec)))
	s.mux.Handle("GET /v1/sessions/{id}/execs/{execID}/asciicast", withTimeout(30*time.Second, http.HandlerFunc(s.exportAsciicast)))
	s.mux.Handle("POST /v1/sessions/{id}/execs/{execID}/signal", withTimeout(10*time.Second, http.HandlerFunc(s.signalExec)))
	s.mux.HandleFunc("GET /v1/sessions/{id}/execs/{execID}/stream", s.streamExec)
	s.mux.Handle("GET /v1/sessions/{id}/files", withTimeout(10*time.Minute, http.HandlerFunc(s.readFile)))
	s.mux.Handle("PUT /v1/sessions/{id}/files", withTimeout(10*time.Minute, http.HandlerFunc(s.writeFile)))
	s.mux.Handle("DELETE /v1/sessions/{id}/files", withTimeout(2*time.Minute, http.HandlerFunc(s.deleteFile)))
	s.mux.Handle("GET /v1/sessions/{id}/files/list", withTimeout(10*time.Minute, http.HandlerFunc(s.listFiles)))
	s.mux.Handle("GET /v1/sessions/{id}/archive", withTimeout(20*time.Minute, http.HandlerFunc(s.downloadArchive)))
	s.mux.Handle("PUT /v1/sessions/{id}/archive", withTimeout(20*time.Minute, http.HandlerFunc(s.uploadArchive)))
	return s.httpMetricsMiddleware(s.idempotencyMiddleware(s.mux))
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
	caps.WarmPool = s.pool != nil && s.cfg.Pool.Enabled && s.cfg.Pool.PerImage > 0 && s.cfg.Pool.MemoryBudgetMB >= 32
	s.doctorOnce.Do(func() {
		s.doctorMu.Lock()
		s.doctorState = "checking"
		s.doctorMu.Unlock()
		go s.loadDoctorChecks()
	})
	s.doctorMu.RLock()
	doctorStatus := s.doctorState
	doctorChecks := append([]string{}, s.doctorInfo...)
	s.doctorMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(System{Backend: s.backendName, Capabilities: caps, DoctorStatus: doctorStatus, DoctorChecks: doctorChecks, EffectiveConfig: effectiveConfig(s.cfg)})
}

func effectiveConfig(cfg config.Config) map[string]any {
	return map[string]any{
		"listen": cfg.Listen, "data_dir": cfg.DataDir, "backend": cfg.Backend, "docker_socket": cfg.DockerSocket,
		"limits": map[string]any{
			"default": map[string]int{"cpu_millicores": cfg.Limits.Default.CPUMillicores, "memory_mb": cfg.Limits.Default.MemoryMB, "pids": cfg.Limits.Default.PIDs, "disk_mb": cfg.Limits.Default.DiskMB, "idle_ttl_s": cfg.Limits.Default.IdleTTLS, "ttl_s": cfg.Limits.Default.TTLS},
			"max":     map[string]int{"cpu_millicores": cfg.Limits.Max.CPUMillicores, "memory_mb": cfg.Limits.Max.MemoryMB, "pids": cfg.Limits.Max.PIDs, "disk_mb": cfg.Limits.Max.DiskMB, "idle_ttl_s": cfg.Limits.Max.IdleTTLS, "ttl_s": cfg.Limits.Max.TTLS},
		},
		"capacity":  map[string]any{"memory_overcommit": cfg.Capacity.MemoryOvercommit, "cpu_overcommit": cfg.Capacity.CPUOvercommit, "queue_wait_s": cfg.Capacity.QueueWaitS, "min_free_disk_mb": cfg.Capacity.MinFreeDiskMB},
		"pool":      map[string]any{"enabled": cfg.Pool.Enabled, "per_image": cfg.Pool.PerImage, "memory_budget_mb": cfg.Pool.MemoryBudgetMB, "images": cfg.Pool.Images},
		"egress":    map[string]any{"allow_internet_mode": cfg.Egress.AllowInternetMode, "extra_blocked_cidrs": cfg.Egress.ExtraBlockedCIDRs, "bandwidth_bytes_per_sec": cfg.Egress.BandwidthBytesPerSec},
		"retention": map[string]int{"output_days": cfg.Retention.OutputDays, "audit_days": cfg.Retention.AuditDays},
		"terminal":  map[string]int{"grace_s": cfg.Terminal.GraceS},
		"log":       map[string]string{"level": cfg.Log.Level, "format": cfg.Log.Format},
		"tls":       map[string]any{"cert": cfg.TLS.Cert, "key_configured": cfg.TLS.Key != ""},
	}
}

func (s *server) loadDoctorChecks() {
	checks := doctor.Check()
	status := summarizeDoctor(checks)
	s.doctorMu.Lock()
	s.doctorInfo = append([]string{}, checks...)
	s.doctorState = status
	s.doctorMu.Unlock()
}

func summarizeDoctor(checks []string) string {
	for _, check := range checks {
		if strings.Contains(check, "(fix:") {
			return "degraded"
		}
	}
	return "ready"
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
