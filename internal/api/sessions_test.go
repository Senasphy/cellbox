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
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/auth"
	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/runtime"
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
	id        string
	killed    bool
	destroyed bool
	files     map[string][]byte
}

type fakeExecHandle struct {
	output chan runtime.OutputChunk
	result runtime.ExecResult
}

func (f *fakeExecHandle) Output() <-chan runtime.OutputChunk               { return f.output }
func (f *fakeExecHandle) Wait(context.Context) (runtime.ExecResult, error) { return f.result, nil }
func (f *fakeExecHandle) Signal(context.Context, int) error                { return runtime.ErrUnsupported }

func (f *fakeSandbox) ID() string { return f.id }
func (f *fakeSandbox) Exec(context.Context, runtime.ExecRequest) (runtime.ExecHandle, error) {
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
	return runtime.Stats{}, runtime.ErrUnsupported
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
	listFiles := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+response.ID+"/files/list?path=%2Ftmp", nil)
	listFiles.Header.Set("Authorization", "Bearer "+user.Secret)
	listedFiles := httptest.NewRecorder()
	handler.ServeHTTP(listedFiles, listFiles)
	if listedFiles.Code != http.StatusOK || !strings.Contains(listedFiles.Body.String(), "note.txt") {
		t.Fatalf("file list status=%d body=%s", listedFiles.Code, listedFiles.Body.String())
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
