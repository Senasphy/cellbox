package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

type sessionLimitsRequest struct {
	CPUMillicores int `json:"cpu_millicores,omitempty"`
	MemoryMB      int `json:"memory_mb,omitempty"`
	PIDs          int `json:"pids,omitempty"`
	DiskMB        int `json:"disk_mb,omitempty"`
	IdleTTLS      int `json:"idle_ttl_s,omitempty"`
	TTLS          int `json:"ttl_s,omitempty"`
}

type createSessionRequest struct {
	Image         string               `json:"image"`
	Limits        sessionLimitsRequest `json:"limits,omitempty"`
	NetworkPolicy map[string]any       `json:"network_policy,omitempty"`
	Env           map[string]string    `json:"env,omitempty"`
	Labels        map[string]string    `json:"labels,omitempty"`
	TTL           int                  `json:"ttl_s,omitempty"`
}

// SessionResponse is the public representation of a sandbox session.
type SessionResponse struct {
	ID             string            `json:"id"`
	Image          string            `json:"image"`
	ImageDigest    string            `json:"image_digest,omitempty"`
	State          string            `json:"state"`
	Limits         runtime.Limits    `json:"limits"`
	NetworkPolicy  map[string]any    `json:"network_policy"`
	Env            map[string]string `json:"env"`
	Labels         map[string]string `json:"labels"`
	CreatedAt      time.Time         `json:"created_at"`
	LastActivityAt time.Time         `json:"last_activity_at"`
	ExpiresAt      time.Time         `json:"expires_at"`
	FailureReason  string            `json:"failure_reason,omitempty"`
}

type sessionPersistence interface {
	CreateSession(context.Context, store.SessionRecord) error
	UpdateSession(context.Context, store.SessionRecord) error
	GetSession(context.Context, string) (store.SessionRecord, error)
	ListSessions(context.Context, string, bool) ([]store.SessionRecord, error)
}
type sandboxResolver interface{ SandboxByID(string) runtime.Sandbox }

func (s *server) createSession(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if key.Scope != "admin" && key.Scope != "session" {
		problem.Write(w, problem.Problem{Title: "Forbidden", Status: http.StatusForbidden, Code: "insufficient_scope"})
		return
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return
	}
	if s.backend == nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable", Detail: "the selected backend is not available"})
		return
	}
	var req createSessionRequest
	if err := decodeJSONBody(w, r, &req, 1<<20); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
		return
	}
	if strings.TrimSpace(req.Image) == "" {
		problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusUnprocessableEntity, Code: "image_required", Detail: "image is required"})
		return
	}
	if len(req.NetworkPolicy) > 0 {
		mode, ok := req.NetworkPolicy["mode"].(string)
		if !ok || mode != "none" || len(req.NetworkPolicy) != 1 {
			problem.Write(w, problem.Problem{Title: "Unsupported network policy", Status: http.StatusUnprocessableEntity, Code: "network_policy_unsupported", Detail: "this backend currently supports only network_policy.mode=none"})
			return
		}
	}
	if err := validateEnvironment(req.Env); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid environment", Status: http.StatusUnprocessableEntity, Code: "env_invalid", Detail: err.Error()})
		return
	}
	caps := runtime.Capabilities{}
	if s.backend != nil {
		caps = s.backend.Capabilities()
	}
	if req.Limits.DiskMB > 0 && !caps.DiskLimits {
		problem.Write(w, problem.Problem{Title: "Unsupported disk limit", Status: http.StatusUnprocessableEntity, Code: "disk_limit_unsupported", Detail: "the selected runtime does not enforce per-session disk limits"})
		return
	}
	limits, err := s.resolveLimits(req)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid limits", Status: http.StatusUnprocessableEntity, Code: "limit_invalid", Detail: err.Error()})
		return
	}
	if !caps.DiskLimits {
		limits.DiskMB = 0
	}
	now := time.Now().UTC()
	sessionID, err := id.New("ses")
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "id_generation_failed"})
		return
	}
	limitsJSON, _ := json.Marshal(limits)
	networkJSON, _ := json.Marshal(nonNilAnyMap(req.NetworkPolicy))
	envJSON, _ := json.Marshal(nonNilMap(req.Env))
	labelsJSON, _ := json.Marshal(nonNilMap(req.Labels))
	record := store.SessionRecord{ID: sessionID, OwnerKeyID: key.ID, ImageReference: req.Image, State: "creating", LimitsJSON: limitsJSON, NetworkPolicyJSON: networkJSON, EnvJSON: envJSON, LabelsJSON: labelsJSON, CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Duration(limits.TTLSeconds) * time.Second)}
	if err = persist.CreateSession(r.Context(), record); err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_persist_failed", Detail: "could not persist session"})
		return
	}
	image, err := s.backend.EnsureImage(r.Context(), req.Image)
	var createdSandbox runtime.Sandbox
	if err == nil {
		record.ImageDigest = image.Digest
		createdSandbox, err = s.backend.Create(r.Context(), runtime.SandboxSpec{ID: sessionID, Image: image, Limits: limits, Env: nonNilMap(req.Env), NetworkMode: "none"})
		if err == nil {
			record.RuntimeID = createdSandbox.ID()
			record.State = "ready"
		}
	}
	if err != nil {
		record.State = "failed"
		record.FailureReason = err.Error()
		record.LastActivityAt = time.Now().UTC()
		failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = persist.UpdateSession(failureCtx, record)
		problem.Write(w, problem.Problem{Title: "Session creation failed", Status: http.StatusBadGateway, Code: "session_create_failed", Detail: err.Error()})
		return
	}
	if err = persist.UpdateSession(r.Context(), record); err != nil {
		if createdSandbox != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = createdSandbox.Destroy(cleanupCtx)
		}
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_persist_failed", Detail: "session started but its ready state could not be stored"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toSessionResponse(record))
}

func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return
	}
	records, err := persist.ListSessions(r.Context(), key.ID, key.Scope == "admin")
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_list_failed"})
		return
	}
	items := make([]SessionResponse, 0, len(records))
	for _, record := range records {
		items = append(items, toSessionResponse(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) getSession(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return
	}
	record, err := persist.GetSession(r.Context(), r.PathValue("id"))
	if err != nil || !canSee(key, record) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "session_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, toSessionResponse(record))
}

func (s *server) deleteSession(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.sessionStore(w)
	if !ok {
		return
	}
	record, err := persist.GetSession(r.Context(), r.PathValue("id"))
	if err != nil || !canSee(key, record) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "session_not_found"})
		return
	}
	if record.State == "stopped" || (record.RuntimeID == "" && (record.State == "expired" || record.State == "failed" || record.State == "lost")) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	resolver, ok := s.backend.(sandboxResolver)
	if !ok || record.RuntimeID == "" {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable", Detail: "the runtime cannot reconnect to this session"})
		return
	}
	box := resolver.SandboxByID(record.RuntimeID)
	record.State = "stopping"
	record.LastActivityAt = time.Now().UTC()
	if err = persist.UpdateSession(r.Context(), record); err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_update_failed"})
		return
	}
	if err = box.Kill(r.Context()); err != nil && !strings.Contains(err.Error(), "returned 404") && !strings.Contains(err.Error(), "returned 409") {
		problem.Write(w, problem.Problem{Title: "Session stop failed", Status: http.StatusBadGateway, Code: "session_stop_failed", Detail: err.Error()})
		return
	}
	if err = box.Destroy(r.Context()); err != nil && !strings.Contains(err.Error(), "returned 404") {
		problem.Write(w, problem.Problem{Title: "Session destroy failed", Status: http.StatusBadGateway, Code: "session_destroy_failed", Detail: err.Error()})
		return
	}
	record.State = "stopped"
	record.LastActivityAt = time.Now().UTC()
	if err = persist.UpdateSession(r.Context(), record); err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_update_failed"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) sessionStore(w http.ResponseWriter) (sessionPersistence, bool) {
	p, ok := s.store.(sessionPersistence)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "store_unavailable"})
		return nil, false
	}
	return p, true
}
func (s *server) resolveLimits(req createSessionRequest) (runtime.Limits, error) {
	d, m := s.cfg.Limits.Default, s.cfg.Limits.Max
	l := runtime.Limits{CPUMillicores: d.CPUMillicores, MemoryMB: d.MemoryMB, PIDs: d.PIDs, DiskMB: d.DiskMB, IdleTTLSeconds: d.IdleTTLS, TTLSeconds: d.TTLS}
	if req.Limits.CPUMillicores != 0 {
		l.CPUMillicores = req.Limits.CPUMillicores
	}
	if req.Limits.MemoryMB != 0 {
		l.MemoryMB = req.Limits.MemoryMB
	}
	if req.Limits.PIDs != 0 {
		l.PIDs = req.Limits.PIDs
	}
	if req.Limits.DiskMB != 0 {
		l.DiskMB = req.Limits.DiskMB
	}
	if req.Limits.IdleTTLS != 0 {
		l.IdleTTLSeconds = req.Limits.IdleTTLS
	}
	if req.Limits.TTLS != 0 {
		l.TTLSeconds = req.Limits.TTLS
	}
	if req.TTL != 0 {
		l.TTLSeconds = req.TTL
	}
	if l.CPUMillicores <= 0 || l.CPUMillicores > m.CPUMillicores || l.MemoryMB <= 0 || l.MemoryMB > m.MemoryMB || l.PIDs <= 0 || l.PIDs > m.PIDs || l.DiskMB <= 0 || l.DiskMB > m.DiskMB || l.TTLSeconds <= 0 || l.TTLSeconds > m.TTLS || l.IdleTTLSeconds <= 0 || l.IdleTTLSeconds > l.TTLSeconds {
		return runtime.Limits{}, errors.New("requested limits must be positive, idle TTL must not exceed TTL, and no limit may exceed the configured maximum")
	}
	return l, nil
}
func decodeJSONBody(w http.ResponseWriter, r *http.Request, out any, max int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}
func validateEnvironment(env map[string]string) error {
	for k, v := range env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return fmt.Errorf("environment variable %q has an invalid name or value", k)
		}
	}
	return nil
}
func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
func canSee(key store.KeyRecord, s store.SessionRecord) bool {
	return key.Scope == "admin" || key.ID == s.OwnerKeyID
}
func toSessionResponse(v store.SessionRecord) SessionResponse {
	var limits runtime.Limits
	var policy map[string]any
	var env, labels map[string]string
	_ = json.Unmarshal(v.LimitsJSON, &limits)
	_ = json.Unmarshal(v.NetworkPolicyJSON, &policy)
	_ = json.Unmarshal(v.EnvJSON, &env)
	_ = json.Unmarshal(v.LabelsJSON, &labels)
	if policy == nil {
		policy = map[string]any{}
	}
	if env == nil {
		env = map[string]string{}
	}
	if labels == nil {
		labels = map[string]string{}
	}
	return SessionResponse{ID: v.ID, Image: v.ImageReference, ImageDigest: v.ImageDigest, State: v.State, Limits: limits, NetworkPolicy: policy, Env: env, Labels: labels, CreatedAt: v.CreatedAt, LastActivityAt: v.LastActivityAt, ExpiresAt: v.ExpiresAt, FailureReason: v.FailureReason}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
