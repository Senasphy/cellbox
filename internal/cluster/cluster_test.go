package cluster

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/api"
	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

type stubRuntime struct {
	boxes map[string]runtime.Sandbox
}

func (*stubRuntime) Name() string                       { return "stub" }
func (*stubRuntime) Capabilities() runtime.Capabilities { return runtime.Capabilities{} }
func (*stubRuntime) EnsureImage(_ context.Context, ref string) (runtime.ImageInfo, error) {
	return runtime.ImageInfo{Reference: ref, Digest: "sha256:stub", Size: 100}, nil
}
func (r *stubRuntime) Create(_ context.Context, spec runtime.SandboxSpec) (runtime.Sandbox, error) {
	box := &stubSandbox{id: spec.ID}
	if r.boxes != nil {
		r.boxes[spec.ID] = box
	}
	return box, nil
}
func (r *stubRuntime) SandboxByID(id string) runtime.Sandbox {
	if r.boxes == nil {
		return nil
	}
	return r.boxes[id]
}
func (*stubRuntime) Reconcile(context.Context) error { return nil }

type stubSandbox struct{ id string }

func (s *stubSandbox) ID() string { return s.id }
func (*stubSandbox) Exec(context.Context, runtime.ExecRequest) (runtime.ExecHandle, error) {
	ch := make(chan runtime.OutputChunk, 1)
	ch <- runtime.OutputChunk{Stream: "stdout", Data: []byte("ok\n")}
	close(ch)
	return &stubExec{ch: ch}, nil
}
func (*stubSandbox) OpenTerminal(context.Context, runtime.TerminalRequest) (runtime.Terminal, error) {
	return nil, runtime.ErrUnsupported
}
func (*stubSandbox) UpdateLimits(context.Context, runtime.Limits) error { return nil }
func (*stubSandbox) ReadFile(context.Context, string) (io.ReadCloser, runtime.FileInfo, error) {
	return nil, runtime.FileInfo{}, runtime.ErrUnsupported
}
func (*stubSandbox) WriteFile(context.Context, string, io.Reader, fs.FileMode) error {
	return runtime.ErrUnsupported
}
func (*stubSandbox) DeleteFile(context.Context, string) error { return runtime.ErrUnsupported }
func (*stubSandbox) ListDir(context.Context, string) ([]runtime.FileInfo, error) {
	return nil, runtime.ErrUnsupported
}
func (*stubSandbox) Archive(context.Context, string) (io.ReadCloser, error) {
	return nil, runtime.ErrUnsupported
}
func (*stubSandbox) Extract(context.Context, string, io.Reader) error { return runtime.ErrUnsupported }
func (*stubSandbox) Snapshot(context.Context) (runtime.SnapshotInfo, error) {
	return runtime.SnapshotInfo{}, runtime.ErrUnsupported
}
func (*stubSandbox) Stats(context.Context) (runtime.Stats, error) {
	return runtime.Stats{}, nil
}
func (*stubSandbox) Kill(context.Context) error    { return nil }
func (*stubSandbox) Destroy(context.Context) error { return nil }

type stubExec struct{ ch chan runtime.OutputChunk }

func (e *stubExec) Output() <-chan runtime.OutputChunk { return e.ch }
func (*stubExec) Wait(context.Context) (runtime.ExecResult, error) {
	return runtime.ExecResult{ExitCode: 0, DurationMS: 1, OutputBytes: 3}, nil
}
func (*stubExec) Signal(context.Context, int) error { return nil }

func TestClusterGatewayThreeWorkersDistributionDrainAndFailover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	admin, err := auth.Generate("key_admin", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}

	type workerNode struct {
		id  string
		srv *httptest.Server
		db  *sqlite.Store
	}
	workers := make([]workerNode, 3)
	for i := 0; i < 3; i++ {
		db, err := sqlite.Open(filepath.Join(t.TempDir(), "worker.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.CreateKey(ctx, sqlite.KeyRecord{
			ID:        admin.ID,
			Name:      admin.Name,
			Prefix:    admin.Prefix,
			Hash:      admin.Hash,
			Scope:     admin.Scope,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		cfg := config.Defaults()
		cfg.DataDir = t.TempDir()
		handler := api.NewHandlerWithConfig(db, &stubRuntime{boxes: map[string]runtime.Sandbox{}}, "stub", cfg)
		srv := httptest.NewServer(handler)
		defer srv.Close()
		workers[i] = workerNode{id: "wrk_" + string(rune('1'+i)), srv: srv, db: db}
	}

	now := time.Now().UTC()
	reg := NewRegistry(nil)
	reg.now = func() time.Time { return now }

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcSrv := NewGRPCServer(reg)
	go func() { _ = grpcSrv.Serve(lis) }()
	defer grpcSrv.Stop()

	client, conn, err := DialGateway(lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for _, w := range workers {
		resp, err := client.RegisterWorker(ctx, &RegisterRequest{
			WorkerID:      w.id,
			AdvertiseURL:  w.srv.URL,
			TotalCPUMilli: 4000,
			TotalMemoryMB: 4096,
			TotalPIDs:     1024,
		})
		if err != nil || !resp.Accepted {
			t.Fatalf("RegisterWorker(%s) = %+v, %v", w.id, resp, err)
		}
	}

	gw := NewGateway(reg, workers[0].srv.Config.Handler, nil)
	gwSrv := httptest.NewServer(gw)
	defer gwSrv.Close()

	createSession := func() (string, int) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gwSrv.URL+"/v1/sessions", strings.NewReader(`{"image":"alpine:latest"}`))
		req.Header.Set("Authorization", "Bearer "+admin.Secret)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out.ID, resp.StatusCode
	}

	// Create 6 sessions; least-loaded scheduler distributes 2 per worker.
	sessionsByWorker := map[string][]string{}
	for i := 0; i < 6; i++ {
		id, code := createSession()
		if code != http.StatusCreated || id == "" {
			t.Fatalf("createSession %d status=%d id=%q", i, code, id)
		}
		w, ok, lost := reg.SessionWorker(ctx, id)
		if !ok || lost {
			t.Fatalf("SessionWorker(%s) ok=%v lost=%v", id, ok, lost)
		}
		sessionsByWorker[w.ID] = append(sessionsByWorker[w.ID], id)
	}
	for _, w := range workers {
		if len(sessionsByWorker[w.id]) != 2 {
			t.Fatalf("worker %s got %d sessions, want 2: %+v", w.id, len(sessionsByWorker[w.id]), sessionsByWorker)
		}
	}

	// Route an exec to a session on wrk_2 through the gateway.
	targetSession := sessionsByWorker["wrk_2"][0]
	execReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, gwSrv.URL+"/v1/sessions/"+targetSession+"/execs?wait=true", strings.NewReader(`{"argv":["echo","hi"]}`))
	execReq.Header.Set("Authorization", "Bearer "+admin.Secret)
	execReq.Header.Set("Content-Type", "application/json")
	execResp, err := http.DefaultClient.Do(execReq)
	if err != nil {
		t.Fatal(err)
	}
	execResp.Body.Close()
	if execResp.StatusCode != http.StatusOK {
		t.Fatalf("exec via gateway status=%d, want 200", execResp.StatusCode)
	}

	// Drain wrk_1 via HTTP; new sessions must not land on wrk_1.
	drainReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, gwSrv.URL+"/v1/cluster/workers/wrk_1/drain", nil)
	drainResp, err := http.DefaultClient.Do(drainReq)
	if err != nil {
		t.Fatal(err)
	}
	drainResp.Body.Close()
	if drainResp.StatusCode != http.StatusOK {
		t.Fatalf("drain status=%d, want 200", drainResp.StatusCode)
	}
	for i := 0; i < 2; i++ {
		id, code := createSession()
		if code != http.StatusCreated {
			t.Fatalf("post-drain create status=%d", code)
		}
		w, _, _ := reg.SessionWorker(ctx, id)
		if w.ID == "wrk_1" {
			t.Fatalf("session %s scheduled on draining worker wrk_1", id)
		}
	}
	// Existing session on wrk_1 remains reachable.
	getReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, gwSrv.URL+"/v1/sessions/"+sessionsByWorker["wrk_1"][0], nil)
	getReq.Header.Set("Authorization", "Bearer "+admin.Secret)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("existing session on drained worker status=%d, want 200", getResp.StatusCode)
	}

	// Simulate wrk_1 and wrk_3 heartbeating while wrk_2 dies.
	now = now.Add(20 * time.Second)
	for _, id := range []string{"wrk_1", "wrk_3"} {
		w, _ := reg.Get(id)
		_, err := client.Heartbeat(ctx, &HeartbeatRequest{
			WorkerID:       w.ID,
			AdvertiseURL:   w.AdvertiseURL,
			TotalCPUMilli:  w.TotalCPUMilli,
			TotalMemoryMB:  w.TotalMemoryMB,
			TotalPIDs:      w.TotalPIDs,
			UsedCPUMilli:   w.UsedCPUMilli,
			UsedMemoryMB:   w.UsedMemoryMB,
			UsedPIDs:       w.UsedPIDs,
			ActiveSessions: w.ActiveSessions,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	workers[1].srv.Close()
	lostWorkers, err := reg.ReapStaleWorkers(ctx, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(lostWorkers) != 1 || lostWorkers[0] != "wrk_2" {
		t.Fatalf("lostWorkers = %v, want [wrk_2]", lostWorkers)
	}
	if !reg.IsSessionLost(targetSession) {
		t.Fatalf("expected session %s on wrk_2 to be marked lost", targetSession)
	}

	// Routing to a lost worker's session returns 503 worker_lost, while new sessions go to wrk_3.
	lostReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, gwSrv.URL+"/v1/sessions/"+targetSession+"/execs", strings.NewReader(`{"argv":["echo"]}`))
	lostReq.Header.Set("Authorization", "Bearer "+admin.Secret)
	lostResp, err := http.DefaultClient.Do(lostReq)
	if err != nil {
		t.Fatal(err)
	}
	lostResp.Body.Close()
	if lostResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("lost worker session status=%d, want 503", lostResp.StatusCode)
	}

	newID, newCode := createSession()
	if newCode != http.StatusCreated {
		t.Fatalf("failover create status=%d", newCode)
	}
	placed, _, _ := reg.SessionWorker(ctx, newID)
	if placed.ID != "wrk_3" {
		t.Fatalf("failover session placed on %s, want wrk_3", placed.ID)
	}
}
