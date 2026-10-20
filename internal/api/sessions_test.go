package api

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
	"github.com/senasphy/cellbox/internal/store/sqlite"
)

type fakeRuntime struct {
	box      *fakeSandbox
	imageErr error
	boxes    map[string]*fakeSandbox
}

type noDiskLimitRuntime struct{ *fakeRuntime }

func (*noDiskLimitRuntime) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{CPULimits: true}
}

func (f *fakeRuntime) Name() string { return "fake" }
func (f *fakeRuntime) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{CPULimits: true, DiskLimits: true}
}
func (f *fakeRuntime) EnsureImage(_ context.Context, ref string) (runtime.ImageInfo, error) {
	if f.imageErr != nil {
		return runtime.ImageInfo{}, f.imageErr
	}
	return runtime.ImageInfo{Reference: ref, Digest: "sha256:test"}, nil
}
func (f *fakeRuntime) Create(_ context.Context, s runtime.SandboxSpec) (runtime.Sandbox, error) {
	f.box = &fakeSandbox{id: "container-test"}
	if f.boxes == nil {
		f.boxes = map[string]*fakeSandbox{}
	}
	f.boxes[f.box.id] = f.box
	return f.box, nil
}
func (f *fakeRuntime) Reconcile(context.Context) error { return nil }
func (f *fakeRuntime) SandboxByID(id string) runtime.Sandbox {
	if f.boxes != nil && f.boxes[id] != nil {
		return f.boxes[id]
	}
	return &fakeSandbox{id: id}
}

type fakeSandbox struct {
	id         string
	killed     bool
	destroyed  bool
	files      map[string][]byte
	execHandle runtime.ExecHandle
	stats      runtime.Stats
}

type fakeExecHandle struct {
	output chan runtime.OutputChunk
	result runtime.ExecResult
}

func (f *fakeExecHandle) Output() <-chan runtime.OutputChunk               { return f.output }
func (f *fakeExecHandle) Wait(context.Context) (runtime.ExecResult, error) { return f.result, nil }
func (f *fakeExecHandle) Signal(context.Context, int) error                { return runtime.ErrUnsupported }

type blockingExecHandle struct {
	output  chan runtime.OutputChunk
	done    chan struct{}
	signals chan int
	once    sync.Once
}

func (f *blockingExecHandle) Output() <-chan runtime.OutputChunk { return f.output }
func (f *blockingExecHandle) Wait(ctx context.Context) (runtime.ExecResult, error) {
	select {
	case <-ctx.Done():
		return runtime.ExecResult{}, ctx.Err()
	case <-f.done:
		return runtime.ExecResult{ExitCode: 0}, nil
	}
}
func (f *blockingExecHandle) Signal(_ context.Context, signal int) error {
	f.signals <- signal
	return runtime.ErrUnsupported
}
func (f *blockingExecHandle) finish() {
	f.once.Do(func() {
		close(f.output)
		close(f.done)
	})
}

func (f *fakeSandbox) ID() string { return f.id }
func (f *fakeSandbox) Exec(context.Context, runtime.ExecRequest) (runtime.ExecHandle, error) {
	if f.execHandle != nil {
		return f.execHandle, nil
	}
	out := make(chan runtime.OutputChunk, 2)
	out <- runtime.OutputChunk{Stream: "stdout", Data: []byte("hello\n")}
	out <- runtime.OutputChunk{Stream: "stderr", Data: []byte("err")}
	close(out)
	return &fakeExecHandle{output: out, result: runtime.ExecResult{ExitCode: 0, OutputBytes: 9}}, nil
}
func (f *fakeSandbox) OpenTerminal(context.Context, runtime.TerminalRequest) (runtime.Terminal, error) {
	return nil, runtime.ErrUnsupported
}
func (f *fakeSandbox) ReadFile(_ context.Context, p string) (io.ReadCloser, runtime.FileInfo, error) {
	data, ok := f.files[p]
	if !ok {
		return nil, runtime.FileInfo{}, errors.New("Docker API returned 404")
	}
	return io.NopCloser(bytes.NewReader(data)), runtime.FileInfo{Path: p, Size: int64(len(data)), Mode: 0644}, nil
}
func (f *fakeSandbox) WriteFile(_ context.Context, p string, r io.Reader, _ fs.FileMode) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[p] = data
	return nil
}
func (f *fakeSandbox) DeleteFile(_ context.Context, p string) error { delete(f.files, p); return nil }
func (f *fakeSandbox) ListDir(context.Context, string) ([]runtime.FileInfo, error) {
	items := []runtime.FileInfo{}
	for name, data := range f.files {
		items = append(items, runtime.FileInfo{Path: name, Size: int64(len(data)), Mode: 0644})
	}
	return items, nil
}
func (f *fakeSandbox) Archive(context.Context, string) (io.ReadCloser, error) {
	var data bytes.Buffer
	tw := tar.NewWriter(&data)
	for name, contents := range f.files {
		if err := tw.WriteHeader(&tar.Header{Name: path.Base(name), Mode: 0644, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(contents); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data.Bytes())), nil
}
func (f *fakeSandbox) Extract(_ context.Context, dest string, r io.Reader) error {
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		contents, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			f.files[path.Join(dest, header.Name)] = contents
		}
	}
}
func (f *fakeSandbox) Snapshot(context.Context) (runtime.SnapshotInfo, error) {
	return runtime.SnapshotInfo{}, runtime.ErrUnsupported
}
func (f *fakeSandbox) UpdateLimits(context.Context, runtime.Limits) error {
	return runtime.ErrUnsupported
}
func (f *fakeSandbox) Stats(context.Context) (runtime.Stats, error) {
	return f.stats, nil
}
func (f *fakeSandbox) Kill(context.Context) error    { f.killed = true; return nil }
func (f *fakeSandbox) Destroy(context.Context) error { f.destroyed = true; return nil }

func TestSessionLifecycleAndOwnership(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	admin, err := auth.Generate("key_admin", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: admin.ID, Name: admin.Name, Prefix: admin.Prefix, Hash: admin.Hash, Scope: admin.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	user, err := auth.Generate("key_user", "user", "session")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: user.ID, Name: user.Name, Prefix: user.Prefix, Hash: user.Hash, Scope: user.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	backend := &fakeRuntime{}
	handler := NewHandlerWithConfig(db, backend, "fake", config.Defaults())
	create := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{"image":"python:3.12","network_policy":{"mode":"none"}}`))
	create.Header.Set("Authorization", "Bearer "+user.Secret)
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var response SessionResponse
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.State != "ready" || response.ImageDigest != "sha256:test" || response.Limits.MemoryMB != 512 {
		t.Fatalf("response=%+v", response)
	}
	backend.box.stats = runtime.Stats{CPUUsageNS: 42, MemoryBytes: 2048, PIDs: 3, IOReadBytes: 4096, IOWriteBytes: 8192}
	metricsRequest := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/metrics", nil)
	metricsRequest.Header.Set("Authorization", "Bearer "+user.Secret)
	metricsResult := httptest.NewRecorder()
	handler.ServeHTTP(metricsResult, metricsRequest)
	var sample sessionMetricsSample
	if metricsResult.Code != http.StatusOK || json.Unmarshal(metricsResult.Body.Bytes(), &sample) != nil {
		t.Fatalf("session metrics status=%d body=%s", metricsResult.Code, metricsResult.Body.String())
	}
	if sample.CPUUsageNS != 42 || sample.MemoryBytes != 2048 || sample.PIDs != 3 || sample.IOReadBytes != 4096 || sample.IOWriteBytes != 8192 || sample.Limits.MemoryMB != 512 {
		t.Fatalf("session metrics sample=%+v", sample)
	}
	streamCtx, cancelMetrics := context.WithCancel(context.Background())
	streamRequest := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/metrics/stream", nil).WithContext(streamCtx)
	streamRequest.Header.Set("Authorization", "Bearer "+user.Secret)
	streamResult := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		handler.ServeHTTP(streamResult, streamRequest)
		close(finished)
	}()
	time.Sleep(500 * time.Millisecond)
	cancelMetrics()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("metrics stream did not close after client cancellation")
	}
	if streamResult.Code != http.StatusOK || !strings.Contains(streamResult.Body.String(), "event: metrics") || !strings.Contains(streamResult.Body.String(), `"cpu_usage_ns":42`) {
		t.Fatalf("metrics stream status=%d body=%s", streamResult.Code, streamResult.Body.String())
	}
	blocking := &blockingExecHandle{output: make(chan runtime.OutputChunk), done: make(chan struct{}), signals: make(chan int, 1)}
	defer blocking.finish()
	backend.box.execHandle = blocking
	startSignalExec := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+response.ID+"/execs", strings.NewReader(`{"argv":["sleep","10"]}`))
	startSignalExec.Header.Set("Authorization", "Bearer "+user.Secret)
	startSignalExec.Header.Set("Content-Type", "application/json")
	startSignalExecResult := httptest.NewRecorder()
	handler.ServeHTTP(startSignalExecResult, startSignalExec)
	backend.box.execHandle = nil
	if startSignalExecResult.Code != http.StatusAccepted {
		blocking.finish()
		t.Fatalf("start signal target status=%d body=%s", startSignalExecResult.Code, startSignalExecResult.Body.String())
	}
	var signalExec struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(startSignalExecResult.Body.Bytes(), &signalExec); err != nil || signalExec.ID == "" {
		blocking.finish()
		t.Fatalf("invalid signal target response: exec=%+v err=%v", signalExec, err)
	}
	signalReq := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+response.ID+"/execs/"+signalExec.ID+"/signal", strings.NewReader(`{"signal":15}`))
	signalReq.Header.Set("Authorization", "Bearer "+user.Secret)
	signalReq.Header.Set("Content-Type", "application/json")
	signalResult := httptest.NewRecorder()
	handler.ServeHTTP(signalResult, signalReq)
	if signalResult.Code != http.StatusUnprocessableEntity || !strings.Contains(signalResult.Body.String(), "capability_unsupported") {
		blocking.finish()
		t.Fatalf("unsupported signal status=%d body=%s", signalResult.Code, signalResult.Body.String())
	}
	select {
	case signal := <-blocking.signals:
		if signal != 15 {
			t.Fatalf("signal=%d, want 15", signal)
		}
	default:
		t.Fatal("signal was not forwarded to the runtime handle")
	}
	blocking.finish()
	filePath := "/v1/sessions/" + response.ID + "/files?path=%2Ftmp%2Fnote.txt"
	put := httptest.NewRequest(http.MethodPut, filePath, strings.NewReader("file contents"))
	put.Header.Set("Authorization", "Bearer "+user.Secret)
	put.Header.Set("X-Cellbox-Mode", "0640")
	putResult := httptest.NewRecorder()
	handler.ServeHTTP(putResult, put)
	if putResult.Code != http.StatusNoContent {
		t.Fatalf("file write status=%d body=%s", putResult.Code, putResult.Body.String())
	}
	getFile := httptest.NewRequest(http.MethodGet, filePath, nil)
	getFile.Header.Set("Authorization", "Bearer "+user.Secret)
	fileResult := httptest.NewRecorder()
	handler.ServeHTTP(fileResult, getFile)
	if fileResult.Code != http.StatusOK || fileResult.Body.String() != "file contents" {
		t.Fatalf("file read status=%d body=%q", fileResult.Code, fileResult.Body.String())
	}
	backend.box.files["/tmp/a.txt"] = []byte("a")
	backend.box.files["/tmp/z.txt"] = []byte("z")
	listFiles := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/files/list?path=%2Ftmp&limit=1", nil)
	listFiles.Header.Set("Authorization", "Bearer "+user.Secret)
	listedFiles := httptest.NewRecorder()
	handler.ServeHTTP(listedFiles, listFiles)
	var filePage struct {
		Items      []runtime.FileInfo `json:"items"`
		NextCursor string             `json:"next_cursor"`
	}
	if err := json.Unmarshal(listedFiles.Body.Bytes(), &filePage); err != nil || listedFiles.Code != http.StatusOK || len(filePage.Items) != 1 || filePage.Items[0].Path != "/tmp/a.txt" || filePage.NextCursor == "" {
		t.Fatalf("file list status=%d body=%s", listedFiles.Code, listedFiles.Body.String())
	}
	fileCursor := filePage.NextCursor
	filePage = struct {
		Items      []runtime.FileInfo `json:"items"`
		NextCursor string             `json:"next_cursor"`
	}{}
	listFilesNext := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/files/list?path=%2Ftmp&limit=1&cursor="+fileCursor, nil)
	listFilesNext.Header.Set("Authorization", "Bearer "+user.Secret)
	listedFilesNext := httptest.NewRecorder()
	handler.ServeHTTP(listedFilesNext, listFilesNext)
	if err := json.Unmarshal(listedFilesNext.Body.Bytes(), &filePage); err != nil || listedFilesNext.Code != http.StatusOK || len(filePage.Items) != 1 || filePage.Items[0].Path != "/tmp/note.txt" || filePage.NextCursor == "" {
		t.Fatalf("second file page status=%d body=%s", listedFilesNext.Code, listedFilesNext.Body.String())
	}
	fileCursor = filePage.NextCursor
	filePage = struct {
		Items      []runtime.FileInfo `json:"items"`
		NextCursor string             `json:"next_cursor"`
	}{}
	listFilesFinal := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/files/list?path=%2Ftmp&limit=1&cursor="+fileCursor, nil)
	listFilesFinal.Header.Set("Authorization", "Bearer "+user.Secret)
	listedFilesFinal := httptest.NewRecorder()
	handler.ServeHTTP(listedFilesFinal, listFilesFinal)
	if err := json.Unmarshal(listedFilesFinal.Body.Bytes(), &filePage); err != nil || listedFilesFinal.Code != http.StatusOK || len(filePage.Items) != 1 || filePage.Items[0].Path != "/tmp/z.txt" || filePage.NextCursor != "" {
		t.Fatalf("final file page status=%d body=%s", listedFilesFinal.Code, listedFilesFinal.Body.String())
	}
	for _, target := range []string{"/v1/sessions/" + response.ID + "/files/list?path=%2Ftmp&limit=201", "/v1/sessions/" + response.ID + "/files/list?path=%2Ftmp&cursor=invalid"} {
		invalidList := httptest.NewRequest(http.MethodGet, target, nil)
		invalidList.Header.Set("Authorization", "Bearer "+user.Secret)
		invalidListResult := httptest.NewRecorder()
		handler.ServeHTTP(invalidListResult, invalidList)
		if invalidListResult.Code != http.StatusBadRequest {
			t.Fatalf("invalid file list query %s status=%d body=%s", target, invalidListResult.Code, invalidListResult.Body.String())
		}
	}
	traversal := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/files?path=..%2F..%2Fetc%2Fpasswd", nil)
	traversal.Header.Set("Authorization", "Bearer "+user.Secret)
	traversalResult := httptest.NewRecorder()
	handler.ServeHTTP(traversalResult, traversal)
	if traversalResult.Code != http.StatusUnprocessableEntity {
		t.Fatalf("path traversal status=%d body=%s", traversalResult.Code, traversalResult.Body.String())
	}
	absoluteTraversal := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/files?path=%2F..%2F..%2Fetc%2Fpasswd", nil)
	absoluteTraversal.Header.Set("Authorization", "Bearer "+user.Secret)
	absoluteTraversalResult := httptest.NewRecorder()
	handler.ServeHTTP(absoluteTraversalResult, absoluteTraversal)
	if absoluteTraversalResult.Code != http.StatusUnprocessableEntity || !strings.Contains(absoluteTraversalResult.Body.String(), "path_outside_sandbox") {
		t.Fatalf("absolute path traversal status=%d body=%s", absoluteTraversalResult.Code, absoluteTraversalResult.Body.String())
	}
	badCwd := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+response.ID+"/execs", strings.NewReader(`{"argv":["pwd"],"cwd":"/../../etc"}`))
	badCwd.Header.Set("Authorization", "Bearer "+user.Secret)
	badCwd.Header.Set("Content-Type", "application/json")
	badCwdResult := httptest.NewRecorder()
	handler.ServeHTTP(badCwdResult, badCwd)
	if badCwdResult.Code != http.StatusUnprocessableEntity || !strings.Contains(badCwdResult.Body.String(), "path_outside_sandbox") {
		t.Fatalf("absolute cwd traversal status=%d body=%s", badCwdResult.Code, badCwdResult.Body.String())
	}
	deleteFile := httptest.NewRequest(http.MethodDelete, filePath, nil)
	deleteFile.Header.Set("Authorization", "Bearer "+user.Secret)
	deleteResult := httptest.NewRecorder()
	handler.ServeHTTP(deleteResult, deleteFile)
	if deleteResult.Code != http.StatusNoContent {
		t.Fatalf("file delete status=%d body=%s", deleteResult.Code, deleteResult.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	list.Header.Set("Authorization", "Bearer "+user.Secret)
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, list)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), response.ID) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	execReq := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+response.ID+"/execs?wait=true", strings.NewReader(`{"argv":["echo","hello"]}`))
	execReq.Header.Set("Authorization", "Bearer "+user.Secret)
	execReq.Header.Set("Content-Type", "application/json")
	execResult := httptest.NewRecorder()
	handler.ServeHTTP(execResult, execReq)
	if execResult.Code != http.StatusOK {
		t.Fatalf("exec status=%d body=%s", execResult.Code, execResult.Body.String())
	}
	var waited waitExecResponse
	if err = json.Unmarshal(execResult.Body.Bytes(), &waited); err != nil {
		t.Fatal(err)
	}
	if waited.Exec.State != "exited" || len(waited.Output) != 2 {
		t.Fatalf("wait response=%+v", waited)
	}
	if waited.Output[0].Data != "aGVsbG8K" || waited.Output[1].Data != "ZXJy" {
		t.Fatalf("wait output=%+v", waited.Output)
	}
	streamReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/execs/"+waited.Exec.ID+"/stream", nil)
	streamReq.Header.Set("Authorization", "Bearer "+user.Secret)
	stream := httptest.NewRecorder()
	handler.ServeHTTP(stream, streamReq)
	if stream.Code != http.StatusOK || !strings.Contains(stream.Body.String(), "event: output") || !strings.Contains(stream.Body.String(), "event: exit") {
		t.Fatalf("stream status=%d body=%s", stream.Code, stream.Body.String())
	}
	resumeReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/execs/"+waited.Exec.ID+"/stream", nil)
	resumeReq.Header.Set("Authorization", "Bearer "+user.Secret)
	resumeReq.Header.Set("Last-Event-ID", "6")
	resumed := httptest.NewRecorder()
	handler.ServeHTTP(resumed, resumeReq)
	if strings.Contains(resumed.Body.String(), "aGVsbG8K") || !strings.Contains(resumed.Body.String(), "ZXJy") {
		t.Fatalf("resume response=%s", resumed.Body.String())
	}
	other, err := auth.Generate("key_other", "other", "session")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: other.ID, Name: other.Name, Prefix: other.Prefix, Hash: other.Hash, Scope: other.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID, nil)
	get.Header.Set("Authorization", "Bearer "+other.Secret)
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, get)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("cross-owner get status=%d", missing.Code)
	}
	del := httptest.NewRequest(http.MethodDelete, "/v1/sessions/"+response.ID, nil)
	del.Header.Set("Authorization", "Bearer "+user.Secret)
	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, del)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	stored, err := db.GetSession(context.Background(), response.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "stopped" {
		t.Fatalf("state=%s", stored.State)
	}
}

func TestSessionCreateIdempotencyReplayAndBodyMismatch(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "idempotency.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := auth.Generate("key_user", "user", "session")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: user.ID, Name: user.Name, Prefix: user.Prefix, Hash: user.Hash, Scope: user.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	backend := &fakeRuntime{}
	handler := NewHandlerWithConfig(db, backend, "fake", config.Defaults())
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+user.Secret)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "create-session-1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"image":"python:3.12","network_policy":{"mode":"none"}}`
	first := request(body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := request(body)
	if second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("replay status=%d body=%s; first status=%d body=%s", second.Code, second.Body.String(), first.Code, first.Body.String())
	}
	if len(backend.boxes) != 1 {
		t.Fatalf("runtime created %d sandboxes; want one", len(backend.boxes))
	}
	changed := request(`{"image":"alpine:3.20","network_policy":{"mode":"none"}}`)
	if changed.Code != http.StatusUnprocessableEntity || !strings.Contains(changed.Body.String(), "idempotency_key_reused") {
		t.Fatalf("mismatched request status=%d body=%s", changed.Code, changed.Body.String())
	}
	if err := db.RevokeKey(context.Background(), user.ID); err != nil {
		t.Fatal(err)
	}
	revokedReplay := request(body)
	if revokedReplay.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key replay status=%d body=%s", revokedReplay.Code, revokedReplay.Body.String())
	}
}

func TestSessionCreateEnforcesKeyQuotaBeforeCapacity(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := auth.Generate("key_quota", "limited", "session")
	if err != nil {
		t.Fatal(err)
	}
	quotas, _ := json.Marshal(map[string]int{"max_sessions": 1})
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: user.ID, Name: user.Name, Prefix: user.Prefix, Hash: user.Hash, Scope: user.Scope, QuotasJSON: quotas, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	backend := &fakeRuntime{}
	handler := NewHandlerWithConfig(db, backend, "fake", config.Defaults())
	create := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{"image":"python:3.12"}`))
		r.Header.Set("Authorization", "Bearer "+user.Secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if first := create(); first.Code != http.StatusCreated {
		t.Fatalf("first create status=%d body=%s", first.Code, first.Body.String())
	}
	limited := create()
	if limited.Code != http.StatusTooManyRequests || !strings.Contains(limited.Body.String(), "quota_exceeded") {
		t.Fatalf("second create status=%d body=%s", limited.Code, limited.Body.String())
	}
	if len(backend.boxes) != 1 {
		t.Fatalf("runtime created %d sandboxes; want one", len(backend.boxes))
	}
}

func TestSessionCreateRejectsWhenCapacityIsCommitted(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "capacity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := auth.Generate("key_capacity", "user", "session")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: user.ID, Name: user.Name, Prefix: user.Prefix, Hash: user.Hash, Scope: user.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Capacity.MemoryOvercommit = 0.000001
	cfg.Capacity.CPUOvercommit = 0.000001
	backend := &fakeRuntime{}
	handler := NewHandlerWithConfig(db, backend, "fake", cfg)
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{"image":"python:3.12"}`))
	r.Header.Set("Authorization", "Bearer "+user.Secret)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "capacity_exceeded") {
		t.Fatalf("create status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	if len(backend.boxes) != 0 {
		t.Fatalf("capacity rejection created %d sandboxes", len(backend.boxes))
	}
}

func TestPrometheusMetricsReportSessionsExecsAndCapacity(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	limits, _ := json.Marshal(runtime.Limits{CPUMillicores: 500, MemoryMB: 256, PIDs: 32})
	session := store.SessionRecord{ID: "ses_metrics", OwnerKeyID: "key_metrics", ImageReference: "alpine", State: "creating", LimitsJSON: limits, NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	session.State = "ready"
	if err := db.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	started := now.Add(time.Second)
	exec := store.ExecRecord{ID: "exe_metrics", SessionID: session.ID, ArgvJSON: []byte(`[]`), EnvJSON: []byte(`{}`), State: "starting", StartedAt: started}
	if err := db.CreateExec(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	ended := started.Add(150 * time.Millisecond)
	exec.State, exec.ExitCode, exec.EndedAt, exec.OutputBytes = "exited", &exitCode, &ended, 12
	if err := db.UpdateExec(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	timedOutExec := store.ExecRecord{ID: "exe_metrics_timeout", SessionID: session.ID, ArgvJSON: []byte(`[]`), EnvJSON: []byte(`{}`), State: "starting", StartedAt: started}
	if err := db.CreateExec(context.Background(), timedOutExec); err != nil {
		t.Fatal(err)
	}
	timedOutExec.State, timedOutExec.EndedAt = "timed_out", &ended
	if err := db.UpdateExec(context.Background(), timedOutExec); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	NewHandlerWithConfig(db, nil, "fake", config.Defaults()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics status=%d content-type=%q body=%s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	for _, metric := range []string{"cellbox_sessions{state=\"ready\"} 1", "cellbox_capacity_committed{resource=\"memory_mb\"} 256", "cellbox_exec_total{result=\"ok\"} 1", "cellbox_output_bytes_total 12", "cellbox_exec_seconds_count 2", "cellbox_session_create_seconds_count{source=\"cold\"} 1", "cellbox_session_create_total 1", "cellbox_error_total 1"} {
		if !strings.Contains(w.Body.String(), metric) {
			t.Errorf("metrics output missing %q:\n%s", metric, w.Body.String())
		}
	}
}

func TestExecCollectorPersistsOutputAndDrains(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "collector.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	session := store.SessionRecord{ID: "ses_collect", OwnerKeyID: "key_collect", ImageReference: "alpine", State: "creating", LimitsJSON: []byte(`{}`), NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	session.State = "ready"
	if err := db.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	handle := &fakeExecHandle{output: make(chan runtime.OutputChunk, 1), result: runtime.ExecResult{ExitCode: 0, OutputBytes: 5}}
	handle.output <- runtime.OutputChunk{Stream: "stdout", Data: []byte("hello")}
	close(handle.output)
	exec := store.ExecRecord{ID: "exe_collect", SessionID: session.ID, ArgvJSON: []byte(`[]`), EnvJSON: []byte(`{}`), State: "starting", StartedAt: now}
	if err := db.CreateExec(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	exec.State = "running"
	if err := db.UpdateExec(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	s := &server{store: db, activeExecs: map[string]runtime.ExecHandle{exec.ID: handle}}
	done := make(chan struct{})
	go s.collectExecOutput(db, handle, exec, done)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exec collector did not drain")
	}
	finished, err := db.GetExec(context.Background(), exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != "exited" || finished.OutputBytes != 5 {
		t.Fatalf("exec after collector=%+v", finished)
	}
	output, err := db.ListExecOutput(context.Background(), exec.ID, 0)
	if err != nil || len(output) != 1 || string(output[0].Data) != "hello" {
		t.Fatalf("persisted output=%+v err=%v", output, err)
	}
	s.execMu.RLock()
	_, active := s.activeExecs[exec.ID]
	s.execMu.RUnlock()
	if active {
		t.Fatal("completed exec remains registered as active")
	}
}

func TestSessionRejectsUnsupportedNetworkAndOutOfRangeLimits(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := auth.Generate("key_a", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithConfig(db, &fakeRuntime{}, "fake", config.Defaults())
	for _, body := range []string{`{"image":"image","network_policy":{"mode":"allow"}}`, `{"image":"image","limits":{"memory_mb":999999}}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

func TestSessionReportsUnsupportedDiskLimitAndRejectsExplicitRequest(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "disk-limit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := auth.Generate("key_disk", "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateKey(context.Background(), sqlite.KeyRecord{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Hash: key.Hash, Scope: key.Scope, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithConfig(db, &noDiskLimitRuntime{fakeRuntime: &fakeRuntime{}}, "docker", config.Defaults())
	systemRequest := httptest.NewRequest(http.MethodGet, "/v1/system", nil)
	systemRequest.Header.Set("Authorization", "Bearer "+key.Secret)
	systemResult := httptest.NewRecorder()
	handler.ServeHTTP(systemResult, systemRequest)
	var system System
	if err := json.Unmarshal(systemResult.Body.Bytes(), &system); err != nil {
		t.Fatal(err)
	}
	if systemResult.Code != http.StatusOK || system.Capabilities.DiskLimits {
		t.Fatalf("system status=%d capabilities=%+v", systemResult.Code, system.Capabilities)
	}
	create := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		req.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	created := create(`{"image":"alpine:latest","network_policy":{"mode":"none"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("default create status=%d body=%s", created.Code, created.Body.String())
	}
	var response SessionResponse
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Limits.DiskMB != 0 {
		t.Fatalf("reported disk limit=%d; want 0 for unsupported quota", response.Limits.DiskMB)
	}
	explicit := create(`{"image":"alpine:latest","limits":{"disk_mb":512}}`)
	if explicit.Code != http.StatusUnprocessableEntity || !strings.Contains(explicit.Body.String(), "disk_limit_unsupported") {
		t.Fatalf("explicit disk request status=%d body=%s", explicit.Code, explicit.Body.String())
	}
}
