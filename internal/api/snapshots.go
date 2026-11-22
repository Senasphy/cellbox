package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/senasphy/cellbox/internal/api/problem"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

type snapshotStore interface {
	CreateSnapshot(context.Context, store.SnapshotRecord) error
	ListSnapshots(context.Context, string) ([]store.SnapshotRecord, error)
	GetSnapshot(context.Context, string) (store.SnapshotRecord, error)
	SnapshotHasChildren(context.Context, string) (bool, error)
	DeleteSnapshot(context.Context, string) error
}

type snapshotRequest struct {
	Labels map[string]string `json:"labels,omitempty"`
}

type forkRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

type snapshotResponse struct {
	ID               string            `json:"id"`
	SourceSessionID  string            `json:"source_session_id"`
	ParentSnapshotID string            `json:"parent_snapshot_id,omitempty"`
	Size             int64             `json:"size_bytes"`
	Flattened        bool              `json:"flattened,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	Labels           map[string]string `json:"labels"`
}

func (s *server) createSnapshot(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	if session.State != "ready" {
		problem.Write(w, problem.Problem{Title: "Session not ready", Status: http.StatusConflict, Code: "session_not_ready"})
		return
	}
	if s.backend == nil || !s.backend.Capabilities().Snapshots {
		problem.Write(w, problem.Problem{Title: "Snapshots unsupported", Status: http.StatusUnprocessableEntity, Code: "snapshot_unsupported"})
		return
	}
	resolver, ok := s.backend.(sandboxResolver)
	if !ok || session.RuntimeID == "" {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
		return
	}
	var req snapshotRequest
	if r.ContentLength != 0 {
		if err := decodeJSONBody(w, r, &req, 1<<20); err != nil {
			problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
			return
		}
	}
	box := resolver.SandboxByID(session.RuntimeID)
	if box == nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
		return
	}
	backendSnapshot, err := s.captureSnapshot(r.Context(), box, session, s.snapshotStore())
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Snapshot failed", Status: http.StatusBadGateway, Code: "snapshot_failed", Detail: err.Error()})
		return
	}
	persist, ok := s.store.(snapshotStore)
	if !ok {
		if manager, supported := s.backend.(runtime.SnapshotManager); supported {
			_ = manager.DeleteSnapshot(context.Background(), backendSnapshot.ID)
		}
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "store_unavailable"})
		return
	}
	snapshotID, err := id.New("snp")
	if err != nil {
		if manager, supported := s.backend.(runtime.SnapshotManager); supported {
			_ = manager.DeleteSnapshot(context.Background(), backendSnapshot.ID)
		}
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "id_generation_failed"})
		return
	}
	labels, _ := json.Marshal(nonNilMap(req.Labels))
	created := time.Now().UTC()
	record := store.SnapshotRecord{ID: snapshotID, SourceSessionID: session.ID, ParentSnapshotID: session.ParentSnapshotID, RuntimeSnapshotID: backendSnapshot.ID, LayerPath: backendSnapshot.ID, Size: backendSnapshot.Size, Flattened: backendSnapshot.Flattened, CreatedAt: created, LabelsJSON: labels}
	if err = persist.CreateSnapshot(r.Context(), record); err != nil {
		if manager, supported := s.backend.(runtime.SnapshotManager); supported {
			_ = manager.DeleteSnapshot(context.Background(), backendSnapshot.ID)
		}
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "snapshot_persist_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, toSnapshotResponse(record))
}

func (s *server) listSessionSnapshots(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	persist, ok := s.snapshotPersistence(w)
	if !ok {
		return
	}
	items, err := persist.ListSnapshots(r.Context(), session.ID)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "snapshot_list_failed"})
		return
	}
	response := make([]snapshotResponse, 0, len(items))
	for _, item := range items {
		response = append(response, toSnapshotResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (s *server) forkSession(w http.ResponseWriter, r *http.Request) {
	key, source, ok := s.authorizedSession(w, r)
	if !ok {
		return
	}
	if source.State != "ready" {
		problem.Write(w, problem.Problem{Title: "Session not ready", Status: http.StatusConflict, Code: "session_not_ready"})
		return
	}
	if s.backend == nil || !s.backend.Capabilities().Snapshots {
		problem.Write(w, problem.Problem{Title: "Fork unsupported", Status: http.StatusUnprocessableEntity, Code: "snapshot_unsupported"})
		return
	}
	var req forkRequest
	snapshots, ok := s.snapshotPersistence(w)
	if !ok {
		return
	}
	if r.ContentLength > 0 {
		if err := decodeJSONBody(w, r, &req, 1<<20); err != nil {
			problem.Write(w, problem.Problem{Title: "Invalid request", Status: http.StatusBadRequest, Code: "request_invalid", Detail: err.Error()})
			return
		}
	}
	var snapshot store.SnapshotRecord
	var err error
	if req.SnapshotID != "" {
		snapshot, err = snapshots.GetSnapshot(r.Context(), req.SnapshotID)
		if err != nil || snapshot.SourceSessionID != source.ID {
			problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "snapshot_not_found"})
			return
		}
	} else {
		resolver, canResolve := s.backend.(sandboxResolver)
		if !canResolve || source.RuntimeID == "" {
			problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
			return
		}
		box := resolver.SandboxByID(source.RuntimeID)
		if box == nil {
			problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
			return
		}
		result, snapshotErr := s.captureSnapshot(r.Context(), box, source, snapshots)
		if snapshotErr != nil {
			problem.Write(w, problem.Problem{Title: "Snapshot failed", Status: http.StatusBadGateway, Code: "snapshot_failed", Detail: snapshotErr.Error()})
			return
		}
		idValue, idErr := id.New("snp")
		if idErr != nil {
			if manager, supported := s.backend.(runtime.SnapshotManager); supported {
				_ = manager.DeleteSnapshot(context.Background(), result.ID)
			}
			problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "id_generation_failed"})
			return
		}
		snapshot = store.SnapshotRecord{ID: idValue, SourceSessionID: source.ID, ParentSnapshotID: source.ParentSnapshotID, RuntimeSnapshotID: result.ID, LayerPath: result.ID, Size: result.Size, Flattened: result.Flattened, CreatedAt: time.Now().UTC(), LabelsJSON: []byte(`{}`)}
		if err = snapshots.CreateSnapshot(r.Context(), snapshot); err != nil {
			if manager, supported := s.backend.(runtime.SnapshotManager); supported {
				_ = manager.DeleteSnapshot(context.Background(), result.ID)
			}
			problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "snapshot_persist_failed"})
			return
		}
	}
	limits := runtime.Limits{}
	policy := runtime.NetworkPolicy{}
	env, labels := map[string]string{}, map[string]string{}
	if err = json.Unmarshal(source.LimitsJSON, &limits); err != nil {
		problem.Write(w, problem.Problem{Title: "Invalid source session", Status: http.StatusInternalServerError, Code: "session_record_invalid"})
		return
	}
	_ = json.Unmarshal(source.NetworkPolicyJSON, &policy)
	_ = json.Unmarshal(source.EnvJSON, &env)
	_ = json.Unmarshal(source.LabelsJSON, &labels)
	if !s.enforceKeyQuota(w, r, key, limits) || !s.enforceCapacity(w, r, limits) {
		return
	}
	if !s.backend.Capabilities().DiskLimits {
		limits.DiskMB = 0
	}
	sessionID, err := id.New("ses")
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "id_generation_failed"})
		return
	}
	now := time.Now().UTC()
	limitsJSON, _ := json.Marshal(limits)
	policyJSON, _ := json.Marshal(policy)
	envJSON, _ := json.Marshal(nonNilMap(env))
	labelsJSON, _ := json.Marshal(nonNilMap(labels))
	child := store.SessionRecord{ID: sessionID, OwnerKeyID: key.ID, ImageReference: source.ImageReference, ImageDigest: source.ImageDigest, State: "creating", LimitsJSON: limitsJSON, NetworkPolicyJSON: policyJSON, EnvJSON: envJSON, LabelsJSON: labelsJSON, CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Duration(limits.TTLSeconds) * time.Second), ParentSnapshotID: snapshot.ID}
	sessionStore, ok := s.sessionStore(w)
	if !ok {
		return
	}
	if err := sessionStore.CreateSession(r.Context(), child); err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_persist_failed"})
		return
	}
	image := runtime.ImageInfo{Reference: source.ImageReference, Digest: source.ImageDigest}
	if s.backend.Name() == "docker" {
		image = runtime.ImageInfo{Reference: snapshot.RuntimeSnapshotID}
	}
	if source.ImageDigest == "" || s.backend.Name() == "docker" {
		image, err = s.backend.EnsureImage(r.Context(), image.Reference)
	}
	var box runtime.Sandbox
	if err == nil {
		var layers []string
		if s.backend.Name() == "native" {
			cursor := snapshot
			for {
				layers = append(layers, cursor.RuntimeSnapshotID)
				if cursor.ParentSnapshotID == "" || cursor.Flattened {
					break
				}
				cursor, err = snapshots.GetSnapshot(r.Context(), cursor.ParentSnapshotID)
				if err != nil {
					break
				}
			}
			if err == nil && len(layers) > 7 {
				err = errors.New("snapshot chain exceeds seven overlay layers; flattening is not available")
			}
		}
		if err == nil {
			policy.ExtraBlockedCIDRs = append([]string(nil), s.cfg.Egress.ExtraBlockedCIDRs...)
			box, err = s.backend.Create(r.Context(), runtime.SandboxSpec{ID: sessionID, Image: image, SnapshotLayers: layers, Limits: limits, Env: nonNilMap(env), NetworkMode: "none", NetworkPolicy: policy})
		}
	}
	if err != nil {
		child.State, child.FailureReason, child.LastActivityAt = "failed", err.Error(), time.Now().UTC()
		failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sessionStore.UpdateSession(failureCtx, child)
		problem.Write(w, problem.Problem{Title: "Fork failed", Status: http.StatusBadGateway, Code: "session_fork_failed", Detail: err.Error()})
		return
	}
	if box == nil {
		err = errors.New("runtime returned an empty forked sandbox")
		child.State, child.FailureReason, child.LastActivityAt = "failed", err.Error(), time.Now().UTC()
		failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sessionStore.UpdateSession(failureCtx, child)
		problem.Write(w, problem.Problem{Title: "Fork failed", Status: http.StatusBadGateway, Code: "session_fork_failed", Detail: err.Error()})
		return
	}
	child.RuntimeID, child.ImageDigest, child.State = box.ID(), image.Digest, "ready"
	if err := sessionStore.UpdateSession(r.Context(), child); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = box.Destroy(cleanupCtx)
		cancel()
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "session_persist_failed"})
		return
	}
	if source, ok := box.(interface {
		LimitEvents() <-chan runtime.LimitBreach
	}); ok {
		go s.watchLimitEvents(sessionID, key.ID, source.LimitEvents())
	}
	if source, ok := box.(interface {
		EgressEvents() <-chan runtime.EgressEvent
	}); ok {
		if events := source.EgressEvents(); events != nil {
			go s.watchEgressEvents(sessionID, key.ID, events)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toSessionResponse(child))
}

func (s *server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.snapshotPersistence(w)
	if !ok {
		return
	}
	record, err := persist.GetSnapshot(r.Context(), r.PathValue("snapshotID"))
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "snapshot_not_found"})
		return
	}
	sessionStore, ok := s.sessionStore(w)
	if !ok {
		return
	}
	source, err := sessionStore.GetSession(r.Context(), record.SourceSessionID)
	if err != nil || !canSee(key, source) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "snapshot_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, toSnapshotResponse(record))
}

func (s *server) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	key, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	persist, ok := s.snapshotPersistence(w)
	if !ok {
		return
	}
	record, err := persist.GetSnapshot(r.Context(), r.PathValue("snapshotID"))
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "snapshot_not_found"})
		return
	}
	sessionStore, ok := s.sessionStore(w)
	if !ok {
		return
	}
	source, err := sessionStore.GetSession(r.Context(), record.SourceSessionID)
	if err != nil || !canSee(key, source) {
		problem.Write(w, problem.Problem{Title: "Not found", Status: http.StatusNotFound, Code: "snapshot_not_found"})
		return
	}
	hasChildren, err := persist.SnapshotHasChildren(r.Context(), record.ID)
	if err != nil {
		problem.Write(w, problem.Problem{Title: "Internal error", Status: http.StatusInternalServerError, Code: "snapshot_read_failed"})
		return
	}
	if hasChildren {
		problem.Write(w, problem.Problem{Title: "Snapshot is in use", Status: http.StatusConflict, Code: "snapshot_in_use", Detail: "delete dependent snapshots and stop live forks before removing this snapshot"})
		return
	}
	if s.backend == nil {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "runtime_unavailable"})
		return
	}
	manager, supported := s.backend.(runtime.SnapshotManager)
	if !supported {
		problem.Write(w, problem.Problem{Title: "Snapshot deletion unsupported", Status: http.StatusUnprocessableEntity, Code: "snapshot_delete_unsupported"})
		return
	}
	if err := manager.DeleteSnapshot(r.Context(), record.RuntimeSnapshotID); err != nil {
		problem.Write(w, problem.Problem{Title: "Snapshot deletion failed", Status: http.StatusBadGateway, Code: "snapshot_delete_failed", Detail: err.Error()})
		return
	}
	if err := persist.DeleteSnapshot(r.Context(), record.ID); err != nil {
		code := "snapshot_delete_failed"
		status := http.StatusInternalServerError
		if errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		problem.Write(w, problem.Problem{Title: "Snapshot deletion failed", Status: status, Code: code, Detail: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) snapshotPersistence(w http.ResponseWriter) (snapshotStore, bool) {
	persist, ok := s.store.(snapshotStore)
	if !ok {
		problem.Write(w, problem.Problem{Title: "Unavailable", Status: http.StatusServiceUnavailable, Code: "store_unavailable"})
		return nil, false
	}
	return persist, true
}

func (s *server) snapshotStore() snapshotStore {
	persist, _ := s.store.(snapshotStore)
	return persist
}

func (s *server) captureSnapshot(ctx context.Context, box runtime.Sandbox, session store.SessionRecord, persist snapshotStore) (runtime.SnapshotInfo, error) {
	if s.backend == nil || s.backend.Name() != "native" || session.ParentSnapshotID == "" {
		return box.Snapshot(ctx)
	}
	if persist == nil {
		return box.Snapshot(ctx)
	}
	depth := 0
	parentID := session.ParentSnapshotID
	for parentID != "" {
		parent, err := persist.GetSnapshot(ctx, parentID)
		if err != nil {
			return runtime.SnapshotInfo{}, err
		}
		depth++
		if parent.Flattened {
			break
		}
		parentID = parent.ParentSnapshotID
	}
	if depth < 7 {
		return box.Snapshot(ctx)
	}
	flattener, ok := box.(runtime.SnapshotFlattener)
	if !ok {
		return runtime.SnapshotInfo{}, errors.New("native runtime cannot flatten this snapshot chain")
	}
	return flattener.SnapshotFlatten(ctx)
}

func toSnapshotResponse(record store.SnapshotRecord) snapshotResponse {
	labels := map[string]string{}
	_ = json.Unmarshal(record.LabelsJSON, &labels)
	if labels == nil {
		labels = map[string]string{}
	}
	return snapshotResponse{ID: record.ID, SourceSessionID: record.SourceSessionID, ParentSnapshotID: record.ParentSnapshotID, Size: record.Size, Flattened: record.Flattened, CreatedAt: record.CreatedAt, Labels: labels}
}
