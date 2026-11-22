package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/runtime/contract"
)

func TestRuntimeContract(t *testing.T) {
	if os.Getenv("CELLBOX_RUN_DOCKER_CONTRACT") != "1" {
		t.Skip("set CELLBOX_RUN_DOCKER_CONTRACT=1 to run the live Docker contract")
	}
	socket := os.Getenv("CELLBOX_DOCKER_SOCKET")
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	if _, err := os.Stat(filepath.Clean(socket)); err != nil {
		t.Fatalf("Docker contract requires an available daemon socket at %s: %v", socket, err)
	}
	image := os.Getenv("CELLBOX_CONTRACT_IMAGE")
	if image == "" {
		image = "alpine:latest"
	}
	contract.RunBasic(t, New(socket), image)
}

func TestReconcileRemovesOnlyLabeledCellboxContainers(t *testing.T) {
	var deleted []string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/containers/json":
			if r.URL.Query().Get("all") != "true" || r.URL.Query().Get("filters") == "" {
				t.Errorf("reconcile list query=%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"Id":"cellbox-1","Labels":{"io.cellbox.session":"ses_a"}},{"Id":"other","Labels":{"other":"value"}}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.41/containers/cellbox-1":
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer engine.Close()
	r := &Runtime{client: engine.Client(), host: engine.URL}
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "/v1.41/containers/cellbox-1" {
		t.Fatalf("deleted containers=%v", deleted)
	}
}

func TestRuntimeContractAgainstEngineStub(t *testing.T) {
	files := map[string][]byte{}
	execs := map[string]struct {
		code   int
		out    []byte
		stderr []byte
		delay  bool
	}{}
	execNumber := 0
	containerKilled := false
	var stateMu sync.Mutex
	const containerID = "contract-container"
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1.41/images/"):
			_, _ = w.Write([]byte(`{"Id":"sha256:contract","RepoDigests":["alpine@sha256:contract"]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/create":
			_, _ = w.Write([]byte(`{"Id":"` + containerID + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/"+containerID+"/start":
			stateMu.Lock()
			containerKilled = false
			stateMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/"+containerID+"/exec":
			stateMu.Lock()
			killed := containerKilled
			stateMu.Unlock()
			if killed {
				http.Error(w, "container is not running", http.StatusConflict)
				return
			}
			var request struct {
				Cmd []string `json:"Cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode exec request: %v", err)
			}
			execNumber++
			execID := fmt.Sprintf("exec-%d", execNumber)
			if len(request.Cmd) > 0 && request.Cmd[0] == "/bin/rm" {
				if len(request.Cmd) > 3 {
					delete(files, request.Cmd[len(request.Cmd)-1])
				}
				execs[execID] = struct {
					code   int
					out    []byte
					stderr []byte
					delay  bool
				}{code: 0}
			} else if strings.Contains(strings.Join(request.Cmd, " "), "contract-stdout") {
				execs[execID] = struct {
					code   int
					out    []byte
					stderr []byte
					delay  bool
				}{code: 0, out: []byte("contract-stdout"), stderr: []byte("contract-stderr")}
			} else if strings.Contains(strings.Join(request.Cmd, " "), "contract-large-output") {
				execs[execID] = struct {
					code   int
					out    []byte
					stderr []byte
					delay  bool
				}{code: 0, out: bytes.Repeat([]byte("x"), 1024)}
			} else if strings.Contains(strings.Join(request.Cmd, " "), "contract-timeout") {
				execs[execID] = struct {
					code   int
					out    []byte
					stderr []byte
					delay  bool
				}{code: 0, delay: true}
			} else {
				execs[execID] = struct {
					code   int
					out    []byte
					stderr []byte
					delay  bool
				}{code: 7, out: []byte("contract-output")}
			}
			_, _ = w.Write([]byte(`{"Id":"` + execID + `"}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1.41/exec/") && strings.HasSuffix(r.URL.Path, "/start"):
			execID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.41/exec/"), "/start")
			if entry, ok := execs[execID]; ok {
				if entry.delay {
					time.Sleep(250 * time.Millisecond)
				}
				if len(entry.out) > 0 {
					writeFrame(t, w, 1, entry.out)
				}
				if len(entry.stderr) > 0 {
					writeFrame(t, w, 2, entry.stderr)
				}
			}
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1.41/exec/") && strings.HasSuffix(r.URL.Path, "/json"):
			execID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.41/exec/"), "/json")
			entry, ok := execs[execID]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprintf(w, `{"ExitCode":%d}`, entry.code)
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/"+containerID+"/update":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Warnings":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/containers/"+containerID+"/stats":
			_, _ = w.Write([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":123}},"memory_stats":{"usage":456},"pids_stats":{"current":2}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/"+containerID+"/kill":
			stateMu.Lock()
			containerKilled = true
			stateMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/containers/"+containerID+"/archive":
			writeEngineArchive(t, w, r.URL.Query().Get("path"), files)
		case r.Method == http.MethodPut && r.URL.Path == "/v1.41/containers/"+containerID+"/archive":
			destination := r.URL.Query().Get("path")
			tr := tar.NewReader(r.Body)
			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("read upload tar: %v", err)
					break
				}
				data, err := io.ReadAll(tr)
				if err != nil {
					t.Errorf("read upload entry: %v", err)
					break
				}
				files[path.Join(destination, header.Name)] = data
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.41/containers/"+containerID:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Engine request: %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected Engine request", http.StatusNotFound)
		}
	}))
	defer engine.Close()
	backend := testRuntime(engine)
	contract.RunBasic(t, backend, "alpine:latest")
}

func writeEngineArchive(t *testing.T, w http.ResponseWriter, target string, files map[string][]byte) {
	t.Helper()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for name, data := range files {
		if target != "/tmp" && target != path.Dir(name) && target != name {
			continue
		}
		archiveName := path.Base(name)
		if target == "/tmp" {
			archiveName = path.Join(strings.TrimPrefix(target, "/"), archiveName)
		}
		if err := tw.WriteHeader(&tar.Header{Name: archiveName, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Errorf("write archive header: %v", err)
			return
		}
		if _, err := tw.Write(data); err != nil {
			t.Errorf("write archive data: %v", err)
			return
		}
		if target != "/tmp" {
			break
		}
	}
	if err := tw.Close(); err != nil {
		t.Errorf("close archive: %v", err)
		return
	}
	_, _ = w.Write(archive.Bytes())
}

func testRuntime(server *httptest.Server) *Runtime {
	return &Runtime{client: server.Client(), host: server.URL}
}

func TestEnsureImageReturnsResolvedDigest(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/images/python:3.12/json") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"sha256:abc","Size":42,"RepoDigests":["python@sha256:abc"]}`))
	}))
	defer s.Close()
	i, err := testRuntime(s).EnsureImage(context.Background(), "python:3.12")
	if err != nil {
		t.Fatal(err)
	}
	if i.Digest != "python@sha256:abc" || i.Size != 42 {
		t.Fatalf("image=%+v", i)
	}
}

func TestImageManagerListsAndRemovesImages(t *testing.T) {
	var removed bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/images/json":
			if r.URL.Query().Get("all") != "true" {
				t.Fatalf("query=%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"Id":"sha256:abc","RepoTags":["alpine:latest"],"Size":42},{"Id":"sha256:empty","RepoTags":["<none>:<none>"],"Size":1}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.41/images/alpine:latest":
			if r.URL.Query().Get("force") != "false" || r.URL.Query().Get("noprune") != "true" {
				t.Fatalf("query=%s", r.URL.RawQuery)
			}
			removed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer s.Close()
	rt := testRuntime(s)
	images, err := rt.ListImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].Reference != "alpine:latest" || images[0].Size != 42 {
		t.Fatalf("images=%+v", images)
	}
	if err := rt.RemoveImage(context.Background(), "alpine:latest"); err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("image was not removed")
	}
}

func TestDestroyTreatsConcurrentRemovalAsComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1.41/containers/container-1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		http.Error(w, `{"message":"removal of container container-1 is already in progress"}`, http.StatusConflict)
	}))
	defer server.Close()
	box := &sandbox{runtime: testRuntime(server), id: "container-1"}
	if err := box.Destroy(context.Background()); err != nil {
		t.Fatalf("concurrent destroy should be idempotent: %v", err)
	}
}

func TestSnapshotUsesDockerCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/v1.41/commit") || r.URL.Query().Get("container") != "c1" {
			t.Fatalf("unexpected snapshot request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"Id":"sha256:snapshot"}`))
	}))
	defer server.Close()
	box := &sandbox{runtime: testRuntime(server), id: "c1"}
	snapshot, err := box.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != "sha256:snapshot" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestCreateAppliesIsolationAndStartsContainer(t *testing.T) {
	var created, started bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/create":
			var body struct {
				HostConfig struct {
					Memory      int64             `json:"Memory"`
					MemorySwap  int64             `json:"MemorySwap"`
					NanoCPUs    int64             `json:"NanoCpus"`
					PIDsLimit   int64             `json:"PidsLimit"`
					NetworkMode string            `json:"NetworkMode"`
					CapDrop     []string          `json:"CapDrop"`
					SecurityOpt []string          `json:"SecurityOpt"`
					StorageOpt  map[string]string `json:"StorageOpt"`
				} `json:"HostConfig"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.HostConfig.Memory != 512<<20 || body.HostConfig.MemorySwap != 512<<20 || body.HostConfig.NanoCPUs != 1_000_000_000 || body.HostConfig.PIDsLimit != 100 || body.HostConfig.NetworkMode != "none" || len(body.HostConfig.CapDrop) != 1 || body.HostConfig.CapDrop[0] != "ALL" || len(body.HostConfig.SecurityOpt) != 1 || len(body.HostConfig.StorageOpt) != 0 {
				t.Errorf("unsafe Docker config: %+v", body.HostConfig)
			}
			created = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"container-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/container-1/start":
			started = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.41/containers/container-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer s.Close()
	b, err := testRuntime(s).Create(context.Background(), runtime.SandboxSpec{ID: "ses_one", Image: runtime.ImageInfo{Reference: "python:3.12"}, Limits: runtime.Limits{MemoryMB: 512, CPUMillicores: 1000, PIDs: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if !created || !started {
		t.Fatalf("container lifecycle create=%v start=%v", created, started)
	}
	if err = b.Destroy(context.Background()); err != nil {
		t.Fatalf("destroy: %v", err)
	}
}

func TestUpdateLimitsAndStats(t *testing.T) {
	updated := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/test-container/update":
			var config struct {
				Memory     int64 `json:"Memory"`
				MemorySwap int64 `json:"MemorySwap"`
				NanoCPUs   int64 `json:"NanoCpus"`
				PIDsLimit  int64 `json:"PidsLimit"`
			}
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				t.Errorf("decode update request: %v", err)
			}
			if config.Memory != 192<<20 || config.MemorySwap != 192<<20 || config.NanoCPUs != 500_000_000 || config.PIDsLimit != 48 {
				t.Errorf("unexpected update config: %+v", config)
			}
			updated = true
			_, _ = w.Write([]byte(`{"Warnings":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/containers/test-container/stats":
			if r.URL.Query().Get("stream") != "false" {
				t.Errorf("expected one-shot stats request, query=%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":123}} ,"memory_stats":{"usage":456},"pids_stats":{"current":2},"blkio_stats":{"io_service_bytes_recursive":[{"op":"Read","value":100},{"op":"Write","value":200},{"op":"Read","value":23}]}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	box := &sandbox{runtime: testRuntime(s), id: "test-container"}
	if err := box.UpdateLimits(context.Background(), runtime.Limits{MemoryMB: 192, CPUMillicores: 500, PIDs: 48}); err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("Docker update request was not sent")
	}
	stats, err := box.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.CPUUsageNS != 123 || stats.MemoryBytes != 456 || stats.PIDs != 2 || stats.IOReadBytes != 123 || stats.IOWriteBytes != 200 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCreateRejectsUnsupportedDiskQuota(t *testing.T) {
	r := &Runtime{}
	_, err := r.Create(context.Background(), runtime.SandboxSpec{
		Image:  runtime.ImageInfo{Reference: "image"},
		Limits: runtime.Limits{MemoryMB: 512, CPUMillicores: 1000, PIDs: 100, DiskMB: 1024},
	})
	if !errors.Is(err, runtime.ErrUnsupported) {
		t.Fatalf("disk limit error=%v, want ErrUnsupported", err)
	}
}

func TestDockerDoesNotClaimDiskQuotaSupport(t *testing.T) {
	if New("").Capabilities().DiskLimits {
		t.Fatal("Docker runtime must not claim unsupported disk quota enforcement")
	}
	box := &sandbox{runtime: &Runtime{}, id: "c1"}
	err := box.UpdateLimits(context.Background(), runtime.Limits{MemoryMB: 512, CPUMillicores: 1000, PIDs: 100, DiskMB: 1024})
	if !errors.Is(err, runtime.ErrUnsupported) {
		t.Fatalf("disk limit update error=%v, want ErrUnsupported", err)
	}
}

func TestCreateRejectsInvalidLimits(t *testing.T) {
	r := &Runtime{}
	if _, err := r.Create(context.Background(), runtime.SandboxSpec{Image: runtime.ImageInfo{Reference: "image"}}); err == nil {
		t.Fatal("expected limit validation")
	}
}

func TestExecStreamsOutputAndExitCode(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/c1/exec":
			_, _ = w.Write([]byte(`{"Id":"e1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/exec/e1/start":
			writeFrame(t, w, 1, []byte("hello\n"))
			writeFrame(t, w, 2, []byte("warning\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/exec/e1/json":
			_, _ = w.Write([]byte(`{"ExitCode":7}`))
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer s.Close()
	box := &sandbox{runtime: testRuntime(s), id: "c1"}
	h, err := box.Exec(context.Background(), runtime.ExecRequest{Argv: []string{"echo", "hello"}, OutputLimit: 64})
	if err != nil {
		t.Fatal(err)
	}
	var streams []string
	for chunk := range h.Output() {
		streams = append(streams, chunk.Stream+":"+string(chunk.Data))
	}
	result, err := h.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 || result.OutputBytes != 14 || len(streams) != 2 || streams[0] != "stdout:hello\n" || streams[1] != "stderr:warning\n" {
		t.Fatalf("result=%+v streams=%q", result, streams)
	}
}

func TestExecOutputCapCountsDiscardedBytes(t *testing.T) {
	var stream bytes.Buffer
	writeBinaryFrame(t, &stream, 1, []byte("1234"))
	writeBinaryFrame(t, &stream, 2, []byte("abc"))
	out := make(chan runtime.OutputChunk, 4)
	total, truncated, err := readExecOutput(&stream, false, 5, out)
	close(out)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for chunk := range out {
		got += string(chunk.Data)
	}
	if total != 7 || !truncated || got != "1234a" {
		t.Fatalf("total=%d truncated=%v output=%q", total, truncated, got)
	}
}

type finalEOFReader struct {
	data []byte
	done bool
}

func (r *finalEOFReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	return copy(p, r.data), io.EOF
}

func TestTTYOutputPreservesBytesReturnedWithEOF(t *testing.T) {
	out := make(chan runtime.OutputChunk, 1)
	total, truncated, err := readExecOutput(&finalEOFReader{data: []byte("final")}, true, 64, out)
	close(out)
	if err != nil {
		t.Fatal(err)
	}
	var output string
	for chunk := range out {
		output += string(chunk.Data)
	}
	if total != 5 || truncated || output != "final" {
		t.Fatalf("total=%d truncated=%v output=%q", total, truncated, output)
	}
}

func TestExecTimeoutKillsDockerContainer(t *testing.T) {
	killed := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.41/containers/c1/exec":
			_, _ = w.Write([]byte(`{"Id":"e1"}`))
		case "/v1.41/exec/e1/start":
			<-release
		case "/v1.41/containers/c1/kill":
			killed <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer func() { close(release); server.Close() }()
	box := &sandbox{runtime: testRuntime(server), id: "c1"}
	if !box.runtime.Capabilities().TimeoutKillsSession {
		t.Fatal("Docker timeout behavior is not advertised")
	}
	handle, err := box.Exec(context.Background(), runtime.ExecRequest{Argv: []string{"sleep", "10"}, TimeoutMS: 10})
	if err != nil {
		t.Fatal(err)
	}
	for range handle.Output() {
	}
	_, err = handle.Wait(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error=%v", err)
	}
	select {
	case <-killed:
	default:
		t.Fatal("Docker container was not killed on timeout")
	}
}

func TestKillIsIdempotentForStoppedContainer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1.41/containers/c1/kill" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "container is not running", http.StatusConflict)
	}))
	defer server.Close()
	if err := (&sandbox{runtime: testRuntime(server), id: "c1"}).Kill(context.Background()); err != nil {
		t.Fatalf("Kill on stopped container: %v", err)
	}
}

func TestTimeoutCleanupRequestHasDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.41/containers/c1/kill" {
			http.NotFound(w, r)
			return
		}
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	r := testRuntime(server)
	err := r.killContainerWithTimeout(context.Background(), "c1", 25*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("kill error=%v, want bounded deadline", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("kill request was not sent")
	}
}

func writeBinaryFrame(t *testing.T, w io.Writer, stream byte, payload []byte) {
	t.Helper()
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
}

func writeFrame(t *testing.T, w http.ResponseWriter, stream byte, payload []byte) {
	t.Helper()
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		t.Error(err)
	}
	if _, err := fmt.Fprint(w, string(payload)); err != nil {
		t.Error(err)
	}
}

func TestCleanPathAndTarTraversalValidation(t *testing.T) {
	for _, p := range []string{"../host", "../../etc/passwd"} {
		if _, err := cleanPath(p); err == nil {
			t.Errorf("accepted escaping path %q", p)
		}
	}
	if p, err := cleanPath("workspace/file.txt"); err != nil || p != "/workspace/file.txt" {
		t.Fatalf("cleanPath=%q, %v", p, err)
	}
	var data bytes.Buffer
	tw := tar.NewWriter(&data)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0600, Size: 0, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateTar(data.Bytes(), "/tmp"); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestSanitizeTarStripsPrivilegedOwnershipAndMode(t *testing.T) {
	var source bytes.Buffer
	tw := tar.NewWriter(&source)
	content := []byte("data")
	if err := tw.WriteHeader(&tar.Header{Name: "payload", Mode: 04755, Uid: 1234, Gid: 5678, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	safe, err := sanitizeTar(source.Bytes(), "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	header, err := tar.NewReader(bytes.NewReader(safe)).Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Mode != 0755 || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
		t.Fatalf("unsafe tar metadata preserved: %+v", header)
	}
}

func TestReadFileFromDockerArchive(t *testing.T) {
	var data bytes.Buffer
	tw := tar.NewWriter(&data)
	payload := []byte("contents")
	if err := tw.WriteHeader(&tar.Header{Name: "file.txt", Mode: 0640, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("X-Docker-Container-Path-Stat", base64.StdEncoding.EncodeToString([]byte(`{"size":8,"mode":416,"isDir":false}`)))
		_, _ = w.Write(data.Bytes())
	}))
	defer s.Close()
	box := &sandbox{runtime: testRuntime(s), id: "c1"}
	reader, info, err := box.ReadFile(context.Background(), "/tmp/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "contents" || info.Size != 8 {
		t.Fatalf("data=%q info=%+v", got, info)
	}
}
