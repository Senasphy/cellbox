//go:build linux

package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	cellruntime "github.com/senasphy/cellbox/internal/runtime"
)

// Runtime runs sessions in user, mount, PID, network, IPC, UTS and cgroup namespaces.
type Runtime struct {
	root    string
	images  *imageStore
	cgroups *CgroupRoot
	mu      sync.RWMutex
	boxes   map[string]*sandbox
}

// NewRuntime creates a native backend below dataDir. cgroups must be a delegated v2 subtree.
func NewRuntime(dataDir string, cgroups *CgroupRoot) (*Runtime, error) {
	var err error
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve native runtime data directory: %w", err)
	}
	if cgroups == nil {
		return nil, errors.New("native runtime requires delegated cgroup v2 controllers")
	}
	for _, dir := range []string{"images", "sessions"} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0700); err != nil {
			return nil, fmt.Errorf("create native %s directory: %w", dir, err)
		}
	}
	return &Runtime{root: dataDir, images: newImageStore(dataDir), cgroups: cgroups, boxes: make(map[string]*sandbox)}, nil
}

func (*Runtime) Name() string { return "native" }

func (*Runtime) Capabilities() cellruntime.Capabilities {
	return cellruntime.Capabilities{CPULimits: true}
}

func (r *Runtime) EnsureImage(ctx context.Context, reference string) (cellruntime.ImageInfo, error) {
	info, _, err := r.images.Ensure(ctx, reference)
	return info, err
}

func (r *Runtime) Create(ctx context.Context, spec cellruntime.SandboxSpec) (cellruntime.Sandbox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if spec.ID == "" || spec.ID == "." || spec.ID == ".." || strings.ContainsAny(spec.ID, `/\`) {
		return nil, fmt.Errorf("invalid native session id %q", spec.ID)
	}
	image := spec.Image
	if image.Reference == "" {
		return nil, errors.New("native session requires an image reference")
	}
	resolved, rootfs, err := r.images.Ensure(ctx, image.Reference)
	if err != nil {
		return nil, err
	}
	if image.Digest != "" && image.Digest != resolved.Digest {
		return nil, fmt.Errorf("image %q changed during session creation: expected %s, resolved %s", image.Reference, image.Digest, resolved.Digest)
	}
	limits := spec.Limits
	if limits.CPUMillicores == 0 || limits.MemoryMB == 0 || limits.PIDs == 0 {
		return nil, errors.New("native session requires positive CPU, memory, and PID limits")
	}
	cgroup, err := r.cgroups.Create(spec.ID, limits)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(r.root, "sessions", spec.ID)
	created := false
	cleanup := func() {
		if created {
			_ = os.RemoveAll(base)
		}
		_ = cgroup.Destroy()
	}
	if err := os.Mkdir(base, 0700); err != nil {
		_ = cgroup.Destroy()
		return nil, fmt.Errorf("create native session directory: %w", err)
	}
	created = true
	for _, dir := range []string{"upper", "work", "root", "run"} {
		if err := os.Mkdir(filepath.Join(base, dir), 0700); err != nil {
			cleanup()
			return nil, fmt.Errorf("create native session %s directory: %w", dir, err)
		}
	}
	cgroupDir, err := os.Open(cgroup.path)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("open native session cgroup: %w", err)
	}
	initConfig := InitConfig{SessionID: spec.ID, LowerDirs: []string{rootfs}, UpperDir: filepath.Join(base, "upper"), WorkDir: filepath.Join(base, "work"), RootDir: filepath.Join(base, "root"), RunDir: filepath.Join(base, "run"), Hostname: spec.ID, Env: spec.Env}
	process, err := SpawnInit(ctx, initConfig, int(cgroupDir.Fd()))
	_ = cgroupDir.Close()
	if err != nil {
		cleanup()
		return nil, err
	}
	box := &sandbox{runtime: r, id: spec.ID, cgroup: cgroup, init: process, dir: base, env: cloneEnv(spec.Env)}
	r.mu.Lock()
	if _, exists := r.boxes[spec.ID]; exists {
		r.mu.Unlock()
		_ = box.Destroy(context.Background())
		return nil, fmt.Errorf("native session %q already exists", spec.ID)
	}
	r.boxes[spec.ID] = box
	r.mu.Unlock()
	return box, nil
}

func (r *Runtime) Reconcile(ctx context.Context) error {
	entries, err := os.ReadDir(r.cgroups.path)
	if err != nil {
		return fmt.Errorf("list native session cgroups: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cgroup := &SessionCgroup{path: filepath.Join(r.cgroups.path, entry.Name())}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := cgroup.Kill(); err != nil {
			return fmt.Errorf("kill orphan native session %s: %w", entry.Name(), err)
		}
		if err := cgroup.WaitEmpty(ctx); err != nil {
			return fmt.Errorf("wait for orphan native session %s: %w", entry.Name(), err)
		}
		if err := cgroup.Destroy(); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(r.root, "sessions", entry.Name())); err != nil {
			return fmt.Errorf("remove orphan native session data %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (r *Runtime) SandboxByID(id string) cellruntime.Sandbox {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.boxes[id]
}

type sandbox struct {
	runtime *Runtime
	id      string
	cgroup  *SessionCgroup
	init    *InitProcess
	dir     string
	env     map[string]string
	mu      sync.Mutex
	done    bool
}

func (s *sandbox) ID() string { return s.id }

func (s *sandbox) Exec(ctx context.Context, request cellruntime.ExecRequest) (cellruntime.ExecHandle, error) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return nil, errors.New("native sandbox has been destroyed")
	}
	s.mu.Unlock()
	if request.TTY {
		return nil, cellruntime.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.Env = mergeEnv(s.env, request.Env)
	handle := &execHandle{output: make(chan cellruntime.OutputChunk, 32), done: make(chan struct{})}
	go func() {
		chunks, result, err := s.init.Exec(context.Background(), request)
		for _, chunk := range chunks {
			handle.output <- chunk
		}
		close(handle.output)
		handle.result, handle.err = result, err
		close(handle.done)
	}()
	return handle, nil
}

func cloneEnv(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func mergeEnv(base, override map[string]string) map[string]string {
	if len(base)+len(override) == 0 {
		return nil
	}
	merged := cloneEnv(base)
	if merged == nil {
		merged = make(map[string]string, len(override))
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

type execHandle struct {
	output chan cellruntime.OutputChunk
	done   chan struct{}
	result cellruntime.ExecResult
	err    error
}

func (h *execHandle) Output() <-chan cellruntime.OutputChunk { return h.output }
func (h *execHandle) Wait(ctx context.Context) (cellruntime.ExecResult, error) {
	select {
	case <-h.done:
		return h.result, h.err
	case <-ctx.Done():
		return cellruntime.ExecResult{}, ctx.Err()
	}
}
func (*execHandle) Signal(context.Context, int) error { return cellruntime.ErrUnsupported }

func (*sandbox) OpenTerminal(context.Context, cellruntime.TerminalRequest) (cellruntime.Terminal, error) {
	return nil, cellruntime.ErrUnsupported
}
func (*sandbox) ReadFile(context.Context, string) (io.ReadCloser, cellruntime.FileInfo, error) {
	return nil, cellruntime.FileInfo{}, cellruntime.ErrUnsupported
}
func (*sandbox) WriteFile(context.Context, string, io.Reader, os.FileMode) error {
	return cellruntime.ErrUnsupported
}
func (*sandbox) DeleteFile(context.Context, string) error { return cellruntime.ErrUnsupported }
func (*sandbox) ListDir(context.Context, string) ([]cellruntime.FileInfo, error) {
	return nil, cellruntime.ErrUnsupported
}
func (*sandbox) Archive(context.Context, string) (io.ReadCloser, error) {
	return nil, cellruntime.ErrUnsupported
}
func (*sandbox) Extract(context.Context, string, io.Reader) error { return cellruntime.ErrUnsupported }
func (*sandbox) Snapshot(context.Context) (cellruntime.SnapshotInfo, error) {
	return cellruntime.SnapshotInfo{}, cellruntime.ErrUnsupported
}
func (s *sandbox) UpdateLimits(_ context.Context, limits cellruntime.Limits) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return errors.New("native sandbox has been destroyed")
	}
	return s.cgroup.UpdateLimits(limits)
}
func (s *sandbox) Stats(_ context.Context) (cellruntime.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return cellruntime.Stats{}, errors.New("native sandbox has been destroyed")
	}
	return s.cgroup.Stats()
}
func (s *sandbox) Kill(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	if err := s.cgroup.Kill(); err != nil {
		return err
	}
	return s.init.Kill()
}
func (s *sandbox) Destroy(ctx context.Context) error {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if err := s.Kill(ctx); err != nil {
		return err
	}
	if err := s.cgroup.WaitEmpty(ctx); err != nil {
		return err
	}
	if err := s.cgroup.Destroy(); err != nil {
		return err
	}
	if err := os.RemoveAll(s.dir); err != nil {
		return err
	}
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	s.runtime.mu.Lock()
	delete(s.runtime.boxes, s.id)
	s.runtime.mu.Unlock()
	return nil
}

var _ cellruntime.Runtime = (*Runtime)(nil)
var _ cellruntime.Sandbox = (*sandbox)(nil)
var _ cellruntime.ExecHandle = (*execHandle)(nil)
