package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

type forkTestRuntime struct {
	*fakeRuntime
	lastSpec runtime.SandboxSpec
}

func (*forkTestRuntime) Name() string { return "native" }
func (*forkTestRuntime) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{Snapshots: true, CPULimits: true, DiskLimits: true}
}
func (f *forkTestRuntime) Create(ctx context.Context, spec runtime.SandboxSpec) (runtime.Sandbox, error) {
	f.lastSpec = spec
	return f.fakeRuntime.Create(ctx, spec)
}

func TestForkSessionUsesSnapshotChainAndReturnsParent(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "fork.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := auth.Generate("key_fork", "fork", "session")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateKey(context.Background(), store.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	limitsJSON := []byte(`{"cpu_millicores":100,"memory_mb":128,"pids":8,"ttl_s":3600}`)
	source := store.SessionRecord{ID: "ses_fork_source", OwnerKeyID: key.ID, ImageReference: "alpine:latest", ImageDigest: "sha256:image", RuntimeID: "source-runtime", State: "ready", LimitsJSON: limitsJSON, NetworkPolicyJSON: []byte(`{"mode":"none"}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.CreateSession(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	parent := store.SnapshotRecord{ID: "snp_parent", SourceSessionID: source.ID, RuntimeSnapshotID: "snp_layer_parent", CreatedAt: now, LabelsJSON: []byte(`{}`)}
	child := store.SnapshotRecord{ID: "snp_child", SourceSessionID: source.ID, ParentSnapshotID: parent.ID, RuntimeSnapshotID: "snp_layer_child", CreatedAt: now.Add(time.Second), LabelsJSON: []byte(`{}`)}
	if err := db.CreateSnapshot(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSnapshot(context.Background(), child); err != nil {
		t.Fatal(err)
	}
	sourceBox := &fakeSandbox{id: source.RuntimeID, snapshot: runtime.SnapshotInfo{ID: "snp_runtime_auto", Size: 64}}
	backend := &forkTestRuntime{fakeRuntime: &fakeRuntime{boxes: map[string]*fakeSandbox{source.RuntimeID: sourceBox}}}
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Capacity.MinFreeDiskMB = 0
	handler := NewHandlerWithConfig(db, backend, "native", cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+source.ID+"/fork", strings.NewReader(`{"snapshot_id":"`+child.ID+`"}`))
	req.Header.Set("Authorization", "Bearer "+key.Secret)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("fork status=%d body=%s", response.Code, response.Body.String())
	}
	var session SessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.ParentSnapshotID != child.ID {
		t.Fatalf("parent snapshot=%q, want %q", session.ParentSnapshotID, child.ID)
	}
	if got := backend.lastSpec.SnapshotLayers; len(got) != 2 || got[0] != child.RuntimeSnapshotID || got[1] != parent.RuntimeSnapshotID {
		t.Fatalf("snapshot layers=%v", got)
	}
	if backend.lastSpec.Image.Reference != source.ImageReference || backend.lastSpec.Image.Digest != source.ImageDigest || backend.lastSpec.Limits.MemoryMB != 128 || backend.lastSpec.NetworkPolicy.Mode != "none" {
		t.Fatalf("fork did not inherit source runtime configuration: %+v", backend.lastSpec)
	}
	created, err := db.GetSession(context.Background(), session.ID)
	if err != nil || created.ParentSnapshotID != child.ID || created.OwnerKeyID != key.ID {
		t.Fatalf("persisted fork=%+v err=%v", created, err)
	}
	autoRequest := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+source.ID+"/fork", nil)
	autoRequest.Header.Set("Authorization", "Bearer "+key.Secret)
	autoResponse := httptest.NewRecorder()
	handler.ServeHTTP(autoResponse, autoRequest)
	if autoResponse.Code != http.StatusCreated {
		t.Fatalf("one-call snapshot fork status=%d body=%s", autoResponse.Code, autoResponse.Body.String())
	}
	var autoSession SessionResponse
	if err := json.Unmarshal(autoResponse.Body.Bytes(), &autoSession); err != nil || autoSession.ParentSnapshotID == "" {
		t.Fatalf("one-call fork response=%+v err=%v", autoSession, err)
	}
	autoSnapshot, err := db.GetSnapshot(context.Background(), autoSession.ParentSnapshotID)
	if err != nil || autoSnapshot.RuntimeSnapshotID != sourceBox.snapshot.ID {
		t.Fatalf("one-call snapshot=%+v err=%v", autoSnapshot, err)
	}
}
