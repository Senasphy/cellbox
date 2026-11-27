package cluster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store/postgres"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WorkerState constants describe worker scheduling eligibility.
const (
	WorkerReady    = "ready"
	WorkerDraining = "draining"
	WorkerLost     = "lost"
)

// ErrNoAvailableWorker is returned when no ready worker has capacity for a session.
var ErrNoAvailableWorker = errors.New("no available cluster worker")

// WorkerInfo describes a registered worker node and its latest heartbeat metrics.
type WorkerInfo struct {
	ID              string    `json:"id"`
	AdvertiseURL    string    `json:"advertise_url"`
	GRPCAddr        string    `json:"grpc_addr,omitempty"`
	State           string    `json:"state"`
	TotalCPUMilli   int64     `json:"total_cpu_milli"`
	TotalMemoryMB   int64     `json:"total_memory_mb"`
	TotalPIDs       int64     `json:"total_pids"`
	UsedCPUMilli    int64     `json:"used_cpu_milli"`
	UsedMemoryMB    int64     `json:"used_memory_mb"`
	UsedPIDs        int64     `json:"used_pids"`
	ActiveSessions  int       `json:"active_sessions"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
}

// PlacementStore persists worker heartbeats and session-to-worker placements.
type PlacementStore interface {
	UpsertWorker(context.Context, postgres.ClusterWorkerRecord) error
	ListWorkers(context.Context) ([]postgres.ClusterWorkerRecord, error)
	MarkWorkerDraining(context.Context, string) error
	MarkLostWorkers(context.Context, time.Time) ([]string, error)
	AssignSessionWorker(context.Context, string, string) error
	GetSessionWorker(context.Context, string) (string, error)
}

// Registry maintains live worker state, session placements, and scheduling decisions.
type Registry struct {
	mu            sync.RWMutex
	workers       map[string]*WorkerInfo
	placements    map[string]string
	lostSessions  map[string]bool
	store         PlacementStore
	nextTieBreak  int
	now           func() time.Time
	onWorkerLost  func(workerID string, sessionIDs []string)
}

// NewRegistry creates a worker registry backed optionally by store.
func NewRegistry(store PlacementStore) *Registry {
	return &Registry{
		workers:      make(map[string]*WorkerInfo),
		placements:   make(map[string]string),
		lostSessions: make(map[string]bool),
		store:        store,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// RegisterWorker implements ClusterServiceServer.
func (r *Registry) RegisterWorker(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	if req == nil || strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.AdvertiseURL) == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id and advertise_url are required")
	}
	parsed, err := url.Parse(req.AdvertiseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, status.Error(codes.InvalidArgument, "advertise_url must be a valid http or https URL")
	}
	now := r.now()
	r.mu.Lock()
	existing, exists := r.workers[req.WorkerID]
	state := WorkerReady
	if exists && existing.State == WorkerDraining {
		state = WorkerDraining
	}
	w := &WorkerInfo{
		ID:              req.WorkerID,
		AdvertiseURL:    strings.TrimRight(req.AdvertiseURL, "/"),
		GRPCAddr:        req.GRPCAddr,
		State:           state,
		TotalCPUMilli:   req.TotalCPUMilli,
		TotalMemoryMB:   req.TotalMemoryMB,
		TotalPIDs:       req.TotalPIDs,
		LastHeartbeatAt: now,
	}
	r.workers[req.WorkerID] = w
	r.mu.Unlock()

	if r.store != nil {
		if err := r.store.UpsertWorker(ctx, toStoreWorker(*w)); err != nil {
			return nil, status.Errorf(codes.Internal, "persist worker registration: %v", err)
		}
	}
	return &RegisterResponse{Accepted: true, State: state}, nil
}

// Heartbeat implements ClusterServiceServer.
func (r *Registry) Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatResponse, error) {
	if req == nil || strings.TrimSpace(req.WorkerID) == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}
	now := r.now()
	r.mu.Lock()
	existing, exists := r.workers[req.WorkerID]
	state := WorkerReady
	if exists && existing.State == WorkerDraining {
		state = WorkerDraining
	}
	advertiseURL := strings.TrimRight(req.AdvertiseURL, "/")
	if advertiseURL == "" && exists {
		advertiseURL = existing.AdvertiseURL
	}
	w := &WorkerInfo{
		ID:              req.WorkerID,
		AdvertiseURL:    advertiseURL,
		GRPCAddr:        req.GRPCAddr,
		State:           state,
		TotalCPUMilli:   req.TotalCPUMilli,
		TotalMemoryMB:   req.TotalMemoryMB,
		TotalPIDs:       req.TotalPIDs,
		UsedCPUMilli:    req.UsedCPUMilli,
		UsedMemoryMB:    req.UsedMemoryMB,
		UsedPIDs:        req.UsedPIDs,
		ActiveSessions:  req.ActiveSessions,
		LastHeartbeatAt: now,
	}
	r.workers[req.WorkerID] = w
	r.mu.Unlock()

	if r.store != nil {
		if err := r.store.UpsertWorker(ctx, toStoreWorker(*w)); err != nil {
			return nil, status.Errorf(codes.Internal, "persist worker heartbeat: %v", err)
		}
	}
	return &HeartbeatResponse{State: state}, nil
}

// DrainWorker implements ClusterServiceServer.
func (r *Registry) DrainWorker(ctx context.Context, req *DrainRequest) (*DrainResponse, error) {
	if req == nil || strings.TrimSpace(req.WorkerID) == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}
	if err := r.Drain(ctx, req.WorkerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "worker not found")
		}
		return nil, status.Errorf(codes.Internal, "drain worker: %v", err)
	}
	return &DrainResponse{State: WorkerDraining}, nil
}

// Drain marks a worker as draining so no new sessions are placed on it.
func (r *Registry) Drain(ctx context.Context, workerID string) error {
	r.mu.Lock()
	w, ok := r.workers[workerID]
	if !ok || w.State == WorkerLost {
		r.mu.Unlock()
		return sql.ErrNoRows
	}
	w.State = WorkerDraining
	r.mu.Unlock()
	if r.store != nil {
		if err := r.store.MarkWorkerDraining(ctx, workerID); err != nil {
			return err
		}
	}
	return nil
}

// List returns a snapshot of all known workers sorted by ID.
func (r *Registry) List() []WorkerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkerInfo, 0, len(r.workers))
	for _, w := range r.workers {
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns a worker by ID.
func (r *Registry) Get(workerID string) (WorkerInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.workers[workerID]
	if !ok {
		return WorkerInfo{}, false
	}
	return *w, true
}

// SelectWorker chooses the least-loaded ready worker with enough headroom for limits.
func (r *Registry) SelectWorker(limits runtime.Limits) (WorkerInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	candidates := make([]*WorkerInfo, 0, len(r.workers))
	for _, w := range r.workers {
		if w.State != WorkerReady {
			continue
		}
		if w.TotalMemoryMB > 0 && w.UsedMemoryMB+int64(limits.MemoryMB) > w.TotalMemoryMB {
			continue
		}
		if w.TotalCPUMilli > 0 && w.UsedCPUMilli+int64(limits.CPUMillicores) > w.TotalCPUMilli {
			continue
		}
		if w.TotalPIDs > 0 && w.UsedPIDs+int64(limits.PIDs) > w.TotalPIDs {
			continue
		}
		candidates = append(candidates, w)
	}
	if len(candidates) == 0 {
		return WorkerInfo{}, ErrNoAvailableWorker
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ActiveSessions != candidates[j].ActiveSessions {
			return candidates[i].ActiveSessions < candidates[j].ActiveSessions
		}
		availI := candidates[i].TotalMemoryMB - candidates[i].UsedMemoryMB
		availJ := candidates[j].TotalMemoryMB - candidates[j].UsedMemoryMB
		if availI != availJ {
			return availI > availJ
		}
		return candidates[i].ID < candidates[j].ID
	})
	chosen := candidates[0]
	// Optimistically account for the placed session until the next heartbeat.
	chosen.ActiveSessions++
	chosen.UsedMemoryMB += int64(limits.MemoryMB)
	chosen.UsedCPUMilli += int64(limits.CPUMillicores)
	chosen.UsedPIDs += int64(limits.PIDs)
	return *chosen, nil
}

// RecordSessionPlacement binds sessionID to workerID.
func (r *Registry) RecordSessionPlacement(ctx context.Context, sessionID, workerID string) error {
	r.mu.Lock()
	r.placements[sessionID] = workerID
	delete(r.lostSessions, sessionID)
	r.mu.Unlock()
	if r.store != nil {
		return r.store.AssignSessionWorker(ctx, sessionID, workerID)
	}
	return nil
}

// SessionWorker returns the worker owning sessionID and whether the session was marked lost.
func (r *Registry) SessionWorker(ctx context.Context, sessionID string) (WorkerInfo, bool, bool) {
	r.mu.RLock()
	lost := r.lostSessions[sessionID]
	workerID, ok := r.placements[sessionID]
	var worker WorkerInfo
	var workerOK bool
	if ok {
		if w, exists := r.workers[workerID]; exists {
			worker = *w
			workerOK = true
			if w.State == WorkerLost {
				lost = true
			}
		}
	}
	r.mu.RUnlock()

	if !ok && r.store != nil {
		if dbWorkerID, err := r.store.GetSessionWorker(ctx, sessionID); err == nil && dbWorkerID != "" {
			r.mu.RLock()
			if w, exists := r.workers[dbWorkerID]; exists {
				worker = *w
				workerOK = true
				if w.State == WorkerLost {
					lost = true
				}
			}
			r.mu.RUnlock()
		}
	}
	return worker, workerOK, lost
}

// IsSessionLost reports whether sessionID belongs to a worker that was marked lost.
func (r *Registry) IsSessionLost(sessionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lostSessions[sessionID]
}

// ReapStaleWorkers marks workers whose last heartbeat is older than timeout as lost,
// and marks all sessions placed on those workers as lost.
func (r *Registry) ReapStaleWorkers(ctx context.Context, timeout time.Duration) ([]string, error) {
	cutoff := r.now().Add(-timeout)
	r.mu.Lock()
	var lostWorkers []string
	for id, w := range r.workers {
		if (w.State == WorkerReady || w.State == WorkerDraining) && w.LastHeartbeatAt.Before(cutoff) {
			w.State = WorkerLost
			lostWorkers = append(lostWorkers, id)
		}
	}
	sort.Strings(lostWorkers)
	lostSet := make(map[string]bool, len(lostWorkers))
	for _, id := range lostWorkers {
		lostSet[id] = true
	}
	affected := make(map[string][]string)
	for sessionID, workerID := range r.placements {
		if lostSet[workerID] {
			r.lostSessions[sessionID] = true
			affected[workerID] = append(affected[workerID], sessionID)
		}
	}
	cb := r.onWorkerLost
	r.mu.Unlock()

	if r.store != nil {
		dbLost, err := r.store.MarkLostWorkers(ctx, cutoff)
		if err != nil {
			return lostWorkers, fmt.Errorf("mark lost workers in store: %w", err)
		}
		for _, id := range dbLost {
			if !lostSet[id] {
				lostWorkers = append(lostWorkers, id)
			}
		}
	}
	if cb != nil {
		for _, workerID := range lostWorkers {
			cb(workerID, affected[workerID])
		}
	}
	return lostWorkers, nil
}

func toStoreWorker(w WorkerInfo) postgres.ClusterWorkerRecord {
	return postgres.ClusterWorkerRecord{
		ID:              w.ID,
		AdvertiseURL:    w.AdvertiseURL,
		GRPCAddr:        w.GRPCAddr,
		State:           w.State,
		TotalCPUMilli:   w.TotalCPUMilli,
		TotalMemoryMB:   w.TotalMemoryMB,
		TotalPIDs:       w.TotalPIDs,
		UsedCPUMilli:    w.UsedCPUMilli,
		UsedMemoryMB:    w.UsedMemoryMB,
		UsedPIDs:        w.UsedPIDs,
		ActiveSessions:  w.ActiveSessions,
		LastHeartbeatAt: w.LastHeartbeatAt,
	}
}
