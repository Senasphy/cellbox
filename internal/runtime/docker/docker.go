// Package docker implements the bootstrap runtime against the Docker Engine HTTP API.
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
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
)

const apiVersion = "v1.41"

// Runtime is a Docker Engine-backed sandbox runtime.
type Runtime struct {
	client *http.Client
	socket string
	host   string
}

// New connects to a Docker-compatible Unix socket.
func New(socket string) *Runtime {
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Runtime{client: &http.Client{Transport: tr}, socket: socket, host: "http://docker"}
}

// Name returns the backend name.
func (*Runtime) Name() string { return "docker" }

// Capabilities reports Docker backend support.
func (*Runtime) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{Snapshots: true, CPULimits: true}
}

// Reconcile is currently a no-op; persistent container ownership labels are added with startup reconciliation.
func (*Runtime) Reconcile(context.Context) error { return nil }

func (r *Runtime) request(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode Docker request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.host+"/"+apiVersion+endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("create Docker request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Docker API %s %s: %w", method, endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Docker API %s %s returned %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

func (r *Runtime) requestReader(ctx context.Context, method, endpoint, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.host+"/"+apiVersion+endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create Docker request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Docker API %s %s: %w", method, endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Docker API %s %s returned %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

// EnsureImage inspects a local image and pulls it if missing.
func (r *Runtime) EnsureImage(ctx context.Context, ref string) (runtime.ImageInfo, error) {
	if strings.TrimSpace(ref) == "" {
		return runtime.ImageInfo{}, errors.New("image reference must not be empty")
	}
	endpoint := "/images/" + url.PathEscape(ref) + "/json"
	resp, err := r.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil && strings.Contains(err.Error(), "returned 404") {
		pull, e := r.request(ctx, http.MethodPost, "/images/create?fromImage="+url.QueryEscape(ref), nil)
		if e != nil {
			return runtime.ImageInfo{}, fmt.Errorf("pull image %q: %w", ref, e)
		}
		var lastErr string
		dec := json.NewDecoder(io.LimitReader(pull.Body, 16<<20))
		for {
			var event struct {
				Error string `json:"error"`
			}
			e = dec.Decode(&event)
			if e == io.EOF {
				break
			}
			if e != nil {
				pull.Body.Close()
				return runtime.ImageInfo{}, fmt.Errorf("read image pull response: %w", e)
			}
			if event.Error != "" {
				lastErr = event.Error
			}
		}
		pull.Body.Close()
		if lastErr != "" {
			return runtime.ImageInfo{}, fmt.Errorf("pull image %q: %s", ref, lastErr)
		}
		resp, err = r.request(ctx, http.MethodGet, endpoint, nil)
	}
	if err != nil {
		return runtime.ImageInfo{}, err
	}
	defer resp.Body.Close()
	var data struct {
		ID          string   `json:"Id"`
		Size        int64    `json:"Size"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return runtime.ImageInfo{}, fmt.Errorf("decode Docker image metadata: %w", err)
	}
	digest := data.ID
	if len(data.RepoDigests) > 0 {
		digest = data.RepoDigests[0]
	}
	return runtime.ImageInfo{Reference: ref, Digest: digest, Size: data.Size}, nil
}

// Create starts an isolated container with the requested resource limits.
func (r *Runtime) Create(ctx context.Context, spec runtime.SandboxSpec) (runtime.Sandbox, error) {
	if spec.Image.Reference == "" {
		return nil, errors.New("image reference is required")
	}
	if spec.Limits.MemoryMB <= 0 || spec.Limits.CPUMillicores <= 0 || spec.Limits.PIDs <= 0 {
		return nil, errors.New("memory, CPU, and PID limits must be positive")
	}
	if spec.Limits.DiskMB > 0 {
		return nil, fmt.Errorf("Docker backend does not enforce per-container disk limits: %w", runtime.ErrUnsupported)
	}
	name := "cellbox-" + strings.TrimPrefix(spec.ID, "ses_")
	if name == "cellbox-" {
		name = fmt.Sprintf("cellbox-%d", time.Now().UnixNano())
	}
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	hostConfig := map[string]any{"Memory": int64(spec.Limits.MemoryMB) << 20, "MemorySwap": int64(spec.Limits.MemoryMB) << 20, "NanoCpus": int64(spec.Limits.CPUMillicores) * 1_000_000, "PidsLimit": spec.Limits.PIDs, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges:true"}, "NetworkMode": "none"}
	config := map[string]any{"Image": spec.Image.Reference, "Cmd": []string{"sleep", "infinity"}, "Env": env, "Labels": map[string]string{"io.cellbox.session": spec.ID}, "HostConfig": hostConfig}
	resp, err := r.request(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(name), config)
	if err != nil {
		return nil, fmt.Errorf("create Docker container: %w", err)
	}
	var created struct {
		ID string `json:"Id"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&created)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("decode Docker container response: %w", err)
	}
	if created.ID == "" {
		return nil, errors.New("Docker returned an empty container id")
	}
	if err = r.simple(ctx, http.MethodPost, "/containers/"+url.PathEscape(created.ID)+"/start", nil); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.simple(cleanupCtx, http.MethodDelete, "/containers/"+url.PathEscape(created.ID)+"?force=true&v=true", nil)
		return nil, fmt.Errorf("start Docker container: %w", err)
	}
	return &sandbox{runtime: r, id: created.ID}, nil
}
func (r *Runtime) simple(ctx context.Context, m, e string, b any) error {
	resp, err := r.request(ctx, m, e, b)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

type sandbox struct {
	runtime *Runtime
	id      string
}

// SandboxByID returns a handle for an existing Docker container ID.
func (r *Runtime) SandboxByID(id string) runtime.Sandbox { return &sandbox{runtime: r, id: id} }

func (s *sandbox) ID() string { return s.id }
func (s *sandbox) Exec(ctx context.Context, req runtime.ExecRequest) (runtime.ExecHandle, error) {
	argv := append([]string(nil), req.Argv...)
	if req.Shell != "" {
		argv = []string{"/bin/sh", "-lc", req.Shell}
	}
	if len(argv) == 0 {
		return nil, errors.New("exec requires argv or shell")
	}
	env := make([]string, 0, len(req.Env))
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	createBody := map[string]any{"AttachStdout": true, "AttachStderr": true, "Tty": req.TTY, "Cmd": argv, "Env": env, "WorkingDir": req.Cwd}
	resp, err := s.runtime.request(ctx, http.MethodPost, "/containers/"+url.PathEscape(s.id)+"/exec", createBody)
	if err != nil {
		return nil, fmt.Errorf("create Docker exec: %w", err)
	}
	var created struct {
		ID string `json:"Id"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&created)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("decode Docker exec id: %w", err)
	}
	if created.ID == "" {
		return nil, errors.New("Docker returned an empty exec id")
	}
	h := &execHandle{output: make(chan runtime.OutputChunk, 32), done: make(chan struct{})}
	go h.run(s.runtime, s.id, created.ID, req)
	return h, nil
}
func (s *sandbox) OpenTerminal(context.Context, runtime.TerminalRequest) (runtime.Terminal, error) {
	return nil, runtime.ErrUnsupported
}
func cleanPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" || strings.ContainsRune(p, 0) {
		return "", errors.New("invalid sandbox path")
	}
	if !path.IsAbs(p) && (p == ".." || strings.HasPrefix(path.Clean(p), "../")) {
		return "", errors.New("path escapes sandbox")
	}
	return path.Clean("/" + p), nil
}
func (s *sandbox) ReadFile(ctx context.Context, p string) (io.ReadCloser, runtime.FileInfo, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return nil, runtime.FileInfo{}, err
	}
	resp, err := s.runtime.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(s.id)+"/archive?path="+url.QueryEscape(clean), nil)
	if err != nil {
		return nil, runtime.FileInfo{}, err
	}
	stat := runtime.FileInfo{Path: clean}
	if raw := resp.Header.Get("X-Docker-Container-Path-Stat"); raw != "" {
		decoded, e := base64.StdEncoding.DecodeString(raw)
		if e == nil {
			var info struct {
				Name  string `json:"name"`
				Size  int64  `json:"size"`
				Mode  int64  `json:"mode"`
				IsDir bool   `json:"isDir"`
			}
			if json.Unmarshal(decoded, &info) == nil {
				stat = runtime.FileInfo{Path: clean, Size: info.Size, Mode: os.FileMode(info.Mode), IsDir: info.IsDir}
			}
		}
	}
	tr := tar.NewReader(resp.Body)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			resp.Body.Close()
			return nil, runtime.FileInfo{}, fmt.Errorf("file %q not found in Docker archive", clean)
		}
		if e != nil {
			resp.Body.Close()
			return nil, runtime.FileInfo{}, fmt.Errorf("read Docker file archive: %w", e)
		}
		if path.Clean("/"+h.Name) == clean || (!strings.Contains(h.Name, "/") && path.Base(clean) == h.Name) {
			if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
				resp.Body.Close()
				return nil, runtime.FileInfo{}, fmt.Errorf("path %q is not a regular file", clean)
			}
			data, e := io.ReadAll(io.LimitReader(tr, 256<<20+1))
			if e != nil {
				resp.Body.Close()
				return nil, runtime.FileInfo{}, e
			}
			resp.Body.Close()
			if len(data) > 256<<20 {
				return nil, runtime.FileInfo{}, errors.New("file exceeds 256 MiB transfer limit")
			}
			stat.Size = int64(len(data))
			return io.NopCloser(bytes.NewReader(data)), stat, nil
		}
	}
}
func (s *sandbox) WriteFile(ctx context.Context, p string, r io.Reader, mode os.FileMode) error {
	clean, err := cleanPath(p)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(r, (256<<20)+1))
	if err != nil {
		return fmt.Errorf("read file payload: %w", err)
	}
	if len(data) > 256<<20 {
		return errors.New("file exceeds 256 MiB transfer limit")
	}
	base := path.Base(clean)
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err = tw.WriteHeader(&tar.Header{Name: base, Mode: int64(mode.Perm() & 0777), Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err == nil {
		_, err = tw.Write(data)
	}
	closeErr := tw.Close()
	if err != nil {
		return fmt.Errorf("create file archive: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	dest := path.Dir(clean)
	resp, err := s.runtime.requestReader(ctx, http.MethodPut, "/containers/"+url.PathEscape(s.id)+"/archive?path="+url.QueryEscape(dest), "application/x-tar", bytes.NewReader(archive.Bytes()))
	if err != nil {
		return fmt.Errorf("write sandbox file: %w", err)
	}
	return resp.Body.Close()
}
func (s *sandbox) ListDir(ctx context.Context, p string) ([]runtime.FileInfo, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return nil, err
	}
	resp, err := s.runtime.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(s.id)+"/archive?path="+url.QueryEscape(clean), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	tr := tar.NewReader(resp.Body)
	result := []runtime.FileInfo{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("read directory archive: %w", e)
		}
		name := path.Clean("/" + h.Name)
		if name == clean {
			continue
		}
		rel := strings.TrimPrefix(name, strings.TrimSuffix(clean, "/")+"/")
		if rel == name || strings.Contains(rel, "/") {
			continue
		}
		result = append(result, runtime.FileInfo{Path: path.Join(clean, rel), Size: h.Size, Mode: os.FileMode(h.Mode), IsDir: h.Typeflag == tar.TypeDir})
	}
	return result, nil
}

func (s *sandbox) DeleteFile(ctx context.Context, p string) error {
	clean, err := cleanPath(p)
	if err != nil {
		return err
	}
	if clean == "/" {
		return errors.New("cannot delete the sandbox root")
	}
	handle, err := s.Exec(ctx, runtime.ExecRequest{Argv: []string{"/bin/rm", "-f", "--", clean}, TimeoutMS: 60_000, OutputLimit: 4096})
	if err != nil {
		return err
	}
	for range handle.Output() {
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("remove file exited with status %d", result.ExitCode)
	}
	return nil
}
func (s *sandbox) Archive(ctx context.Context, p string) (io.ReadCloser, error) {
	clean, err := cleanPath(p)
	if err != nil {
		return nil, err
	}
	resp, err := s.runtime.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(s.id)+"/archive?path="+url.QueryEscape(clean), nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
func (s *sandbox) Extract(ctx context.Context, p string, r io.Reader) error {
	clean, err := cleanPath(p)
	if err != nil {
		return err
	}
	reader, writer := io.Pipe()
	sanitizeDone := make(chan error, 1)
	go func() {
		sanitizeErr := sanitizeTarStream(r, writer, clean)
		if sanitizeErr != nil {
			_ = writer.CloseWithError(sanitizeErr)
		} else {
			_ = writer.Close()
		}
		sanitizeDone <- sanitizeErr
	}()
	resp, err := s.runtime.requestReader(ctx, http.MethodPut, "/containers/"+url.PathEscape(s.id)+"/archive?path="+url.QueryEscape(clean), "application/x-tar", reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		_ = <-sanitizeDone
		return fmt.Errorf("extract sandbox archive: %w", err)
	}
	closeErr := resp.Body.Close()
	sanitizeErr := <-sanitizeDone
	if sanitizeErr != nil {
		return sanitizeErr
	}
	return closeErr
}

func validateTar(data []byte, destination string) error {
	_, err := sanitizeTar(data, destination)
	return err
}

func sanitizeTar(data []byte, destination string) ([]byte, error) {
	var sanitized bytes.Buffer
	err := sanitizeTarStream(bytes.NewReader(data), &sanitized, destination)
	if err != nil {
		return nil, err
	}
	return sanitized.Bytes(), nil
}

type boundedReader struct {
	reader io.Reader
	read   int64
	limit  int64
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if r.read >= r.limit {
		return 0, io.EOF
	}
	if int64(len(p)) > r.limit-r.read {
		p = p[:r.limit-r.read]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func sanitizeTarStream(source io.Reader, destination io.Writer, destPath string) error {
	bounded := &boundedReader{reader: source, limit: (512 << 20) + 1}
	tr := tar.NewReader(bounded)
	tw := tar.NewWriter(destination)
	count := 0
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			if _, err = io.Copy(io.Discard, bounded); err != nil {
				return fmt.Errorf("read trailing archive data: %w", err)
			}
			if bounded.read > 512<<20 {
				return errors.New("archive exceeds 512 MiB transfer limit")
			}
			if err = tw.Close(); err != nil {
				return fmt.Errorf("finish sanitized tar: %w", err)
			}
			return nil
		}
		if err != nil {
			if bounded.read > 512<<20 {
				return errors.New("archive exceeds 512 MiB transfer limit")
			}
			return fmt.Errorf("invalid tar archive: %w", err)
		}
		count++
		if count > 100000 {
			return errors.New("archive has too many entries")
		}
		name := strings.TrimPrefix(h.Name, "./")
		if name == "" || path.IsAbs(name) || name == ".." || strings.HasPrefix(path.Clean(name), "../") {
			return fmt.Errorf("archive path %q escapes its destination", h.Name)
		}
		full := path.Clean(path.Join(destPath, name))
		if full != destPath && !strings.HasPrefix(full, strings.TrimSuffix(destPath, "/")+"/") {
			return fmt.Errorf("archive path %q escapes its destination", h.Name)
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return fmt.Errorf("archive entry %q has unsupported type", h.Name)
		}
		total += h.Size
		if h.Size < 0 || total > 512<<20 {
			return errors.New("archive exceeds 512 MiB expanded size limit")
		}
		cleanHeader := *h
		cleanHeader.Name = name
		cleanHeader.Mode = h.Mode & 0777
		cleanHeader.Uid = 0
		cleanHeader.Gid = 0
		cleanHeader.Uname = ""
		cleanHeader.Gname = ""
		cleanHeader.Linkname = ""
		cleanHeader.PAXRecords = nil
		cleanHeader.Xattrs = nil
		if err = tw.WriteHeader(&cleanHeader); err != nil {
			return fmt.Errorf("write sanitized tar header: %w", err)
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			if _, err = io.CopyN(tw, tr, h.Size); err != nil {
				return fmt.Errorf("copy archive entry data: %w", err)
			}
		}
	}
}
func (s *sandbox) Snapshot(ctx context.Context) (runtime.SnapshotInfo, error) {
	resp, err := s.runtime.request(ctx, http.MethodPost, "/commit?container="+url.QueryEscape(s.id)+"&repo=cellbox-snapshots&tag="+url.QueryEscape(s.id), nil)
	if err != nil {
		return runtime.SnapshotInfo{}, fmt.Errorf("commit Docker snapshot: %w", err)
	}
	defer resp.Body.Close()
	var image struct {
		ID string `json:"Id"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&image); err != nil {
		return runtime.SnapshotInfo{}, fmt.Errorf("decode Docker snapshot: %w", err)
	}
	return runtime.SnapshotInfo{ID: image.ID}, nil
}
func (s *sandbox) UpdateLimits(ctx context.Context, l runtime.Limits) error {
	if l.MemoryMB <= 0 || l.CPUMillicores <= 0 || l.PIDs <= 0 {
		return errors.New("memory, CPU, and PID limits must be positive")
	}
	if l.DiskMB > 0 {
		return fmt.Errorf("Docker backend does not enforce per-container disk limits: %w", runtime.ErrUnsupported)
	}
	config := map[string]any{"Memory": int64(l.MemoryMB) << 20, "MemorySwap": int64(l.MemoryMB) << 20, "NanoCpus": int64(l.CPUMillicores) * 1_000_000, "PidsLimit": l.PIDs}
	return s.runtime.simple(ctx, http.MethodPost, "/containers/"+url.PathEscape(s.id)+"/update", config)
}
func (s *sandbox) Stats(ctx context.Context) (runtime.Stats, error) {
	resp, err := s.runtime.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(s.id)+"/stats?stream=false", nil)
	if err != nil {
		return runtime.Stats{}, err
	}
	defer resp.Body.Close()
	var raw struct {
		CPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
		} `json:"cpu_stats"`
		Memory struct {
			Usage uint64 `json:"usage"`
		} `json:"memory_stats"`
		PIDs struct {
			Current int `json:"current"`
		} `json:"pids_stats"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&raw); err != nil {
		return runtime.Stats{}, fmt.Errorf("decode Docker stats: %w", err)
	}
	return runtime.Stats{CPUUsageNS: raw.CPU.Usage.Total, MemoryBytes: raw.Memory.Usage, PIDs: raw.PIDs.Current}, nil
}
func (s *sandbox) Kill(ctx context.Context) error {
	return s.runtime.simple(ctx, http.MethodPost, "/containers/"+url.PathEscape(s.id)+"/kill", nil)
}
func (s *sandbox) Destroy(ctx context.Context) error {
	return s.runtime.simple(ctx, http.MethodDelete, "/containers/"+url.PathEscape(s.id)+"?force=true&v=true", nil)
}

var _ runtime.Runtime = (*Runtime)(nil)
var _ runtime.Sandbox = (*sandbox)(nil)

type execHandle struct {
	output chan runtime.OutputChunk
	done   chan struct{}
	mu     sync.RWMutex
	result runtime.ExecResult
	err    error
}

func (h *execHandle) Output() <-chan runtime.OutputChunk { return h.output }
func (h *execHandle) Wait(ctx context.Context) (runtime.ExecResult, error) {
	select {
	case <-ctx.Done():
		return runtime.ExecResult{}, ctx.Err()
	case <-h.done:
		h.mu.RLock()
		defer h.mu.RUnlock()
		return h.result, h.err
	}
}
func (h *execHandle) Signal(context.Context, int) error { return runtime.ErrUnsupported }
func (h *execHandle) run(r *Runtime, containerID, id string, req runtime.ExecRequest) {
	start := time.Now()
	defer close(h.output)
	defer close(h.done)
	execCtx := context.Background()
	cancel := func() {}
	if req.TimeoutMS > 0 {
		execCtx, cancel = context.WithTimeout(execCtx, time.Duration(req.TimeoutMS)*time.Millisecond)
	}
	defer cancel()
	resp, err := r.request(execCtx, http.MethodPost, "/exec/"+url.PathEscape(id)+"/start", map[string]any{"Detach": false, "Tty": req.TTY})
	var total int64
	var truncated bool
	if err == nil {
		total, truncated, err = readExecOutput(resp.Body, req.TTY, req.OutputLimit, h.output)
		_ = resp.Body.Close()
	}
	result := runtime.ExecResult{ExitCode: -1, DurationMS: time.Since(start).Milliseconds(), OutputBytes: total, Truncated: truncated}
	if err == nil {
		inspect, e := r.request(execCtx, http.MethodGet, "/exec/"+url.PathEscape(id)+"/json", nil)
		if e != nil {
			err = e
		} else {
			var state struct {
				ExitCode int `json:"ExitCode"`
			}
			e = json.NewDecoder(io.LimitReader(inspect.Body, 1<<20)).Decode(&state)
			inspect.Body.Close()
			if e != nil {
				err = fmt.Errorf("decode Docker exec result: %w", e)
			} else {
				result.ExitCode = state.ExitCode
			}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		result.ExitCode = 137
		result.Signal = 9
		_ = r.simple(context.Background(), http.MethodPost, "/containers/"+url.PathEscape(containerID)+"/kill", nil)
	}
	h.mu.Lock()
	h.result = result
	h.err = err
	h.mu.Unlock()
}
func readExecOutput(r io.Reader, tty bool, capBytes int64, out chan<- runtime.OutputChunk) (int64, bool, error) {
	var total int64
	truncated := false
	for {
		var stream byte
		var payload []byte
		if tty {
			buf := make([]byte, 32<<10)
			n, err := r.Read(buf)
			if n > 0 {
				stream = 1
				payload = buf[:n]
			}
			if err == io.EOF {
				return total, truncated, nil
			}
			if err != nil {
				return total, truncated, err
			}
		} else {
			header := make([]byte, 8)
			_, err := io.ReadFull(r, header)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				if err == io.EOF {
					return total, truncated, nil
				}
				return total, truncated, fmt.Errorf("incomplete Docker output frame: %w", err)
			}
			if err != nil {
				return total, truncated, err
			}
			stream = header[0]
			size := binary.BigEndian.Uint32(header[4:])
			if size > 16<<20 {
				return total, truncated, fmt.Errorf("Docker output frame exceeds 16 MiB: %d", size)
			}
			payload = make([]byte, size)
			if _, err = io.ReadFull(r, payload); err != nil {
				return total, truncated, fmt.Errorf("read Docker output frame: %w", err)
			}
		}
		if stream != 1 && stream != 2 {
			continue
		}
		allowed := int64(len(payload))
		if capBytes > 0 && total+allowed > capBytes {
			allowed = capBytes - total
			if allowed < 0 {
				allowed = 0
			}
			truncated = true
		}
		if allowed > 0 {
			name := "stdout"
			if stream == 2 {
				name = "stderr"
			}
			out <- runtime.OutputChunk{Stream: name, Data: append([]byte(nil), payload[:allowed]...)}
		}
		total += int64(len(payload))
	}
}
