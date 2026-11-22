//go:build linux

package native

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/senasphy/cellbox/internal/egress"
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
	for _, dir := range []string{"images", "sessions", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0700); err != nil {
			return nil, fmt.Errorf("create native %s directory: %w", dir, err)
		}
	}
	return &Runtime{root: dataDir, images: newImageStore(dataDir), cgroups: cgroups, boxes: make(map[string]*sandbox)}, nil
}

func (*Runtime) Name() string { return "native" }

func (*Runtime) Capabilities() cellruntime.Capabilities {
	return cellruntime.Capabilities{Snapshots: true, CPULimits: true, DiskLimits: true, TTY: true, EgressPolicy: true}
}

func (r *Runtime) EnsureImage(ctx context.Context, reference string) (cellruntime.ImageInfo, error) {
	info, _, _, err := r.images.Ensure(ctx, reference)
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
	if len(spec.SnapshotLayers) > 7 {
		return nil, fmt.Errorf("native snapshot chain has %d layers; flattening is required above seven", len(spec.SnapshotLayers))
	}
	resolved, rootfs, imageEnv, err := r.images.Ensure(ctx, image.Reference)
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
	if limits.DiskMB < 0 || int64(limits.DiskMB) > (1<<63-1)/(1<<20) {
		return nil, errors.New("native session disk limit is invalid")
	}
	policy := egress.Policy{Mode: spec.NetworkPolicy.Mode, Allow: make([]egress.Rule, 0, len(spec.NetworkPolicy.Allow)), ExtraBlockedCIDRs: spec.NetworkPolicy.ExtraBlockedCIDRs}
	for _, rule := range spec.NetworkPolicy.Allow {
		policy.Allow = append(policy.Allow, egress.Rule{Host: rule.Host, Ports: rule.Ports})
	}
	if err := egress.ValidatePolicy(policy); err != nil {
		return nil, fmt.Errorf("invalid native egress policy: %w", err)
	}
	cgroup, err := r.cgroups.Create(spec.ID, limits)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(r.root, "sessions", spec.ID)
	created := false
	var egressProxy *egress.Server
	var egressCancel context.CancelFunc
	var egressQueue *egressEventQueue
	cleanup := func() {
		if egressCancel != nil {
			egressCancel()
		}
		if egressProxy != nil {
			_ = egressProxy.Close()
		}
		if egressQueue != nil {
			egressQueue.Close()
		}
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
	helperMountpoint, err := os.OpenFile(filepath.Join(base, "run", "cellbox-exec"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create native exec helper mountpoint: %w", err)
	}
	if err := helperMountpoint.Close(); err != nil {
		cleanup()
		return nil, fmt.Errorf("close native exec helper mountpoint: %w", err)
	}
	cgroupDir, err := os.Open(cgroup.path)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("open native session cgroup: %w", err)
	}
	env := cloneEnv(imageEnv)
	if env == nil {
		env = make(map[string]string, len(spec.Env))
	}
	for key, value := range spec.Env {
		env[key] = value
	}
	if spec.NetworkPolicy.Mode != "" && spec.NetworkPolicy.Mode != "none" {
		egressQueue = newEgressEventQueue()
		egressProxy, err = egress.New(filepath.Join(base, "run", "egress.sock"), policy, func(event egress.Decision) {
			egressQueue.Publish(cellruntime.EgressEvent{Host: event.Host, Port: event.Port, Resolved: event.Resolved, Verdict: event.Verdict, Reason: event.Reason, BytesUp: event.BytesUp, BytesDown: event.BytesDown, Duration: event.Duration.Milliseconds(), Timestamp: event.Timestamp})
		})
		if err != nil {
			cleanup()
			return nil, err
		}
		proxyCtx, cancel := context.WithCancel(context.Background())
		egressCancel = cancel
		if err := egressProxy.Start(proxyCtx); err != nil {
			cleanup()
			return nil, err
		}
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			env[key] = "http://127.0.0.1:3128"
		}
		env["NO_PROXY"], env["no_proxy"] = "localhost,127.0.0.1", "localhost,127.0.0.1"
	}
	lowerDirs := make([]string, 0, len(spec.SnapshotLayers)+1)
	for _, snapshotID := range spec.SnapshotLayers {
		if snapshotID == "" || snapshotID == "." || snapshotID == ".." || strings.ContainsAny(snapshotID, `/\\`) || !strings.HasPrefix(snapshotID, "snp_") {
			cleanup()
			return nil, fmt.Errorf("invalid native snapshot layer id %q", snapshotID)
		}
		layer := filepath.Join(r.root, "snapshots", snapshotID, "upper")
		info, statErr := os.Stat(layer)
		if statErr != nil || !info.IsDir() {
			cleanup()
			return nil, fmt.Errorf("native snapshot layer %q is unavailable", snapshotID)
		}
		lowerDirs = append(lowerDirs, layer)
	}
	lowerDirs = append(lowerDirs, rootfs)
	initConfig := InitConfig{SessionID: spec.ID, LowerDirs: lowerDirs, UpperDir: filepath.Join(base, "upper"), WorkDir: filepath.Join(base, "work"), RootDir: filepath.Join(base, "root"), RunDir: filepath.Join(base, "run"), Hostname: spec.ID, Env: env, Egress: egressProxy != nil}
	process, err := SpawnInit(ctx, initConfig, int(cgroupDir.Fd()))
	_ = cgroupDir.Close()
	if err != nil {
		cleanup()
		return nil, err
	}
	box := &sandbox{runtime: r, id: spec.ID, cgroup: cgroup, init: process, dir: base, env: cloneEnv(env), egressProxy: egressProxy, egressCancel: egressCancel, egressEvents: egressQueue}
	box.startDiskMonitor(limits.DiskMB)
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
	runtime        *Runtime
	id             string
	cgroup         *SessionCgroup
	init           *InitProcess
	dir            string
	env            map[string]string
	mu             sync.Mutex
	destroyMu      sync.Mutex
	done           bool
	killed         bool
	diskLimitBytes atomic.Uint64
	diskHit        atomic.Bool
	diskEvents     chan cellruntime.LimitBreach
	diskStop       chan struct{}
	diskDone       chan struct{}
	diskStopOnce   sync.Once
	inFlightExecs  atomic.Int32
	egressProxy    *egress.Server
	egressCancel   context.CancelFunc
	egressEvents   *egressEventQueue
}

type egressEventQueue struct {
	mu     sync.Mutex
	open   bool
	events chan cellruntime.EgressEvent
}

func newEgressEventQueue() *egressEventQueue {
	return &egressEventQueue{open: true, events: make(chan cellruntime.EgressEvent, 256)}
}

func (q *egressEventQueue) Publish(event cellruntime.EgressEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.open {
		return
	}
	q.events <- event
}

func (q *egressEventQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.open {
		q.open = false
		close(q.events)
	}
}

func (s *sandbox) EgressEvents() <-chan cellruntime.EgressEvent {
	if s.egressEvents == nil {
		return nil
	}
	return s.egressEvents.events
}

var errUpperUsageLimit = errors.New("native upper directory usage exceeded limit")

// LimitEvents exposes asynchronous breaches found by the native resource monitor.
func (s *sandbox) LimitEvents() <-chan cellruntime.LimitBreach { return s.diskEvents }

func (s *sandbox) startDiskMonitor(limitMB int) {
	s.diskEvents = make(chan cellruntime.LimitBreach, 8)
	s.diskStop = make(chan struct{})
	s.diskDone = make(chan struct{})
	if limitMB > 0 {
		s.diskLimitBytes.Store(uint64(limitMB) * 1024 * 1024)
	}
	go s.monitorDiskUsage()
}

func (s *sandbox) monitorDiskUsage() {
	defer close(s.diskDone)
	defer close(s.diskEvents)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastStats, _ := s.cgroup.Stats()
	for {
		select {
		case <-s.diskStop:
			return
		case <-ticker.C:
		}
		if stats, err := s.cgroup.Stats(); err == nil {
			if s.inFlightExecs.Load() == 0 {
				var event cellruntime.LimitBreach
				switch {
				case stats.OOMKills > lastStats.OOMKills:
					event = cellruntime.LimitBreach{Kind: "memory", Counter: stats.OOMKills, Reason: "oom_kill", Timestamp: time.Now().UTC()}
				case stats.PIDsMaxEvents > lastStats.PIDsMaxEvents:
					event = cellruntime.LimitBreach{Kind: "pids", Counter: stats.PIDsMaxEvents, Reason: "pids_limit", Timestamp: time.Now().UTC()}
				}
				if event.Kind != "" {
					s.stopForResourceLimit()
					s.diskEvents <- event
					return
				}
			}
			lastStats = stats
		}
		limit := s.diskLimitBytes.Load()
		if limit == 0 {
			continue
		}
		used, err := upperDirectoryUsage(filepath.Join(s.dir, "upper"), s.diskStop, limit)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
		}
		overLimit := errors.Is(err, errUpperUsageLimit)
		measurementFailed := err != nil && !overLimit
		if (!overLimit && !measurementFailed && used <= limit) || !s.diskHit.CompareAndSwap(false, true) {
			continue
		}
		if measurementFailed {
			used = 0
		}
		s.stopForResourceLimit()
		reason := "quota_exceeded"
		if measurementFailed {
			reason = "measurement_failed"
		}
		s.diskEvents <- cellruntime.LimitBreach{Kind: "disk", UsedBytes: used, LimitBytes: limit, Reason: reason, Timestamp: time.Now().UTC()}
		return
	}
}

func (s *sandbox) stopForResourceLimit() {
	_ = s.cgroup.Freeze()
	if err := s.Kill(context.Background()); err != nil {
		// Namespace init may already have exited after a kernel resource kill.
		_ = s.init.Kill()
		s.mu.Lock()
		s.killed = true
		s.mu.Unlock()
	}
}

func upperDirectoryUsage(root string, stop <-chan struct{}, limit uint64) (uint64, error) {
	var used uint64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-stop:
			return context.Canceled
		default:
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 0 {
			size := uint64(info.Size())
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Blocks >= 0 {
				size = uint64(stat.Blocks) * 512
			}
			if ^uint64(0)-used < size {
				return errors.New("native upper directory usage overflow")
			}
			used += size
			if used > limit {
				return errUpperUsageLimit
			}
		}
		return nil
	})
	return used, err
}

func (s *sandbox) stopDiskMonitor() {
	s.diskStopOnce.Do(func() { close(s.diskStop) })
	<-s.diskDone
}

func (s *sandbox) ID() string { return s.id }

func (s *sandbox) Exec(ctx context.Context, request cellruntime.ExecRequest) (cellruntime.ExecHandle, error) {
	s.mu.Lock()
	if s.done || s.killed {
		s.mu.Unlock()
		return nil, errors.New("native sandbox is no longer running")
	}
	s.mu.Unlock()
	if request.TTY {
		return nil, cellruntime.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.Env = mergeEnv(s.env, request.Env)
	s.inFlightExecs.Add(1)
	before, beforeErr := s.cgroup.Stats()
	signals := make(chan int, 8)
	handle := &execHandle{output: make(chan cellruntime.OutputChunk, 32), done: make(chan struct{})}
	handle.signal = func(ctx context.Context, number int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if number < 1 || number > 64 {
			return fmt.Errorf("signal number %d is outside 1..64", number)
		}
		select {
		case <-handle.done:
			return errors.New("exec is no longer running")
		case signals <- number:
			return nil
		default:
			return errors.New("too many pending signals for exec")
		}
	}
	go func() {
		defer s.inFlightExecs.Add(-1)
		result, err := s.init.Exec(context.Background(), request, func(chunk cellruntime.OutputChunk) error {
			handle.output <- chunk
			return nil
		}, signals)
		if beforeErr == nil {
			if after, statsErr := s.cgroup.Stats(); statsErr == nil {
				if after.OOMKills > before.OOMKills {
					result.OOMKilled = true
					err = nil
				}
				result.PIDsLimitHit = after.PIDsMaxEvents > before.PIDsMaxEvents
			}
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
	signal func(context.Context, int) error
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
func (h *execHandle) Signal(ctx context.Context, signal int) error {
	if h.signal == nil {
		return cellruntime.ErrUnsupported
	}
	return h.signal(ctx, signal)
}

func (s *sandbox) OpenTerminal(ctx context.Context, request cellruntime.TerminalRequest) (cellruntime.Terminal, error) {
	s.mu.Lock()
	if s.done || s.killed {
		s.mu.Unlock()
		return nil, errors.New("native sandbox is no longer running")
	}
	s.mu.Unlock()
	request.Env = mergeEnv(s.env, request.Env)
	return s.init.OpenTerminal(ctx, request)
}
func (s *sandbox) UpdateLimits(_ context.Context, limits cellruntime.Limits) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return errors.New("native sandbox has been destroyed")
	}
	if limits.DiskMB < 0 || int64(limits.DiskMB) > (1<<63-1)/(1<<20) {
		return errors.New("native disk limit is invalid")
	}
	if err := s.cgroup.UpdateLimits(limits); err != nil {
		return err
	}
	if limits.DiskMB == 0 {
		s.diskLimitBytes.Store(0)
	} else {
		s.diskLimitBytes.Store(uint64(limits.DiskMB) * 1024 * 1024)
	}
	return nil
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
	if err := s.init.Kill(); err != nil {
		return err
	}
	s.killed = true
	return nil
}
func (s *sandbox) Destroy(ctx context.Context) error {
	s.destroyMu.Lock()
	defer s.destroyMu.Unlock()
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	s.stopDiskMonitor()
	if err := s.Kill(ctx); err != nil {
		return err
	}
	if err := s.cgroup.WaitEmpty(ctx); err != nil {
		return err
	}
	if err := s.cgroup.Destroy(); err != nil {
		return err
	}
	if s.egressCancel != nil {
		s.egressCancel()
	}
	if s.egressProxy != nil {
		if err := s.egressProxy.Close(); err != nil {
			return err
		}
	}
	if s.egressEvents != nil {
		s.egressEvents.Close()
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
