// Package pool manages a bounded cache of initialized, pristine sandboxes.
package pool

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/senasphy/cellbox/internal/config"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/runtime"
)

const (
	warmMemoryMB = 32
	warmCPUMilli = 100
	warmPIDs     = 32
	warmMaxAge   = 10 * time.Minute
)

type item struct {
	box       runtime.Sandbox
	image     runtime.ImageInfo
	createdAt time.Time
}

// Runtime adds warm-pool claiming to a runtime without changing the Sandbox contract.
type Manager struct {
	backend  runtime.Runtime
	settings config.Pool
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	items    map[string][]item
	pending  map[string]int
	demand   map[string]runtime.SandboxSpec
	retry    map[string]time.Duration
	usedMB   int
	closed   bool
	wg       sync.WaitGroup
}

func New(backend runtime.Runtime, settings config.Pool) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Manager{backend: backend, settings: settings, ctx: ctx, cancel: cancel, items: map[string][]item{}, pending: map[string]int{}, demand: map[string]runtime.SandboxSpec{}, retry: map[string]time.Duration{}}
	if settings.Enabled && settings.PerImage > 0 && settings.MemoryBudgetMB >= warmMemoryMB {
		r.wg.Add(1)
		go r.recycleLoop()
	}
	return r
}

// Prime asynchronously pulls and fills the configured curated image pools.
func (r *Manager) Prime(images []string) {
	r.mu.Lock()
	if r.closed || !r.settings.Enabled || r.settings.PerImage == 0 || r.settings.MemoryBudgetMB < warmMemoryMB || len(images) == 0 {
		r.mu.Unlock()
		return
	}
	imageLimit := r.settings.MemoryBudgetMB / warmMemoryMB
	if imageLimit < len(images) {
		images = images[:imageLimit]
	}
	for range images {
		r.wg.Add(1)
	}
	r.mu.Unlock()
	for _, reference := range images {
		go r.primeImage(reference)
	}
}

func (r *Manager) primeImage(reference string) {
	defer r.wg.Done()
	delay := time.Second
	for r.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(r.ctx, 15*time.Minute)
		image, err := r.backend.EnsureImage(ctx, reference)
		cancel()
		if err == nil {
			r.Refill(runtime.SandboxSpec{Image: image, NetworkMode: "none", NetworkPolicy: runtime.NetworkPolicy{Mode: "none"}})
			return
		}
		jitter := time.Duration(rand.Int63n(int64(delay/2)+1)) - delay/4
		timer := time.NewTimer(delay + jitter)
		select {
		case <-r.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay *= 2
		if delay > time.Minute {
			delay = time.Minute
		}
	}
}

// Claim atomically removes a compatible warm sandbox and applies the requested limits.
func (r *Manager) Claim(ctx context.Context, spec runtime.SandboxSpec) (runtime.Sandbox, bool) {
	if !r.eligible(spec) {
		return nil, false
	}
	key := imageKey(spec.Image)
	for {
		r.mu.Lock()
		list := r.items[key]
		if len(list) == 0 {
			r.demand[key] = warmSpec(spec)
			r.mu.Unlock()
			return nil, false
		}
		candidate := list[len(list)-1]
		r.items[key] = list[:len(list)-1]
		r.usedMB -= warmMemoryMB
		r.demand[key] = warmSpec(spec)
		r.mu.Unlock()
		if time.Since(candidate.createdAt) > warmMaxAge {
			destroyWarm(candidate.box)
			continue
		}
		if err := candidate.box.UpdateLimits(ctx, spec.Limits); err != nil {
			destroyWarm(candidate.box)
			continue
		}
		r.Refill(spec)
		return candidate.box, true
	}
}

// Refill starts bounded asynchronous creation for the requested image.
func (r *Manager) Refill(spec runtime.SandboxSpec) {
	if !r.eligible(spec) {
		return
	}
	key := imageKey(spec.Image)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.demand[key] = warmSpec(spec)
	r.startRefillLocked(key)
	r.mu.Unlock()
}

func (r *Manager) startRefillLocked(key string) {
	for len(r.items[key])+r.pending[key] < r.settings.PerImage && r.usedMB+warmMemoryMB <= r.settings.MemoryBudgetMB {
		spec, ok := r.demand[key]
		if !ok || r.closed {
			return
		}
		r.pending[key]++
		r.usedMB += warmMemoryMB
		r.wg.Add(1)
		go r.createWarm(key, spec)
	}
}

func (r *Manager) createWarm(key string, spec runtime.SandboxSpec) {
	defer r.wg.Done()
	timer := time.NewTimer(time.Duration(rand.Int63n(int64(500 * time.Millisecond))))
	select {
	case <-r.ctx.Done():
		timer.Stop()
		r.mu.Lock()
		r.pending[key]--
		r.usedMB -= warmMemoryMB
		r.mu.Unlock()
		return
	case <-timer.C:
	}
	spec = warmSpec(spec)
	newID, err := id.New("ses")
	if err == nil {
		spec.ID = newID
	}
	ctx, cancel := context.WithTimeout(r.ctx, 15*time.Minute)
	defer cancel()
	var box runtime.Sandbox
	if err == nil {
		box, err = r.backend.Create(ctx, spec)
		if err == nil && box == nil {
			err = errors.New("runtime returned a nil warm sandbox")
		}
	}
	r.mu.Lock()
	r.pending[key]--
	if err != nil || r.closed || ctx.Err() != nil {
		r.usedMB -= warmMemoryMB
		retry := err != nil && !r.closed && ctx.Err() == nil
		r.mu.Unlock()
		if box != nil {
			destroyWarm(box)
		}
		if retry {
			r.scheduleRetry(key)
		}
		return
	}
	delete(r.retry, key)
	r.items[key] = append(r.items[key], item{box: box, image: spec.Image, createdAt: time.Now()})
	r.startRefillLocked(key)
	r.mu.Unlock()
}

func (r *Manager) scheduleRetry(key string) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	delay := r.retry[key]
	if delay == 0 {
		delay = time.Second
	} else {
		delay *= 2
		if delay > time.Minute {
			delay = time.Minute
		}
	}
	r.retry[key] = delay
	r.mu.Unlock()
	jitter := time.Duration(rand.Int63n(int64(delay/2)+1)) - delay/4
	time.AfterFunc(delay+jitter, func() {
		r.mu.Lock()
		r.startRefillLocked(key)
		r.mu.Unlock()
	})
}

func (r *Manager) recycleLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case now := <-ticker.C:
			type staleGroup struct {
				key   string
				items []item
			}
			var stale []staleGroup
			r.mu.Lock()
			for key, group := range r.items {
				fresh := group[:0]
				var expired []item
				for _, value := range group {
					if now.Sub(value.createdAt) >= warmMaxAge {
						expired = append(expired, value)
					} else {
						fresh = append(fresh, value)
					}
				}
				if len(expired) > 0 {
					r.items[key] = fresh
					r.usedMB -= len(expired) * warmMemoryMB
					stale = append(stale, staleGroup{key: key, items: expired})
				}
			}
			r.mu.Unlock()
			for _, group := range stale {
				for _, value := range group.items {
					destroyWarm(value.box)
				}
				r.mu.Lock()
				r.startRefillLocked(group.key)
				r.mu.Unlock()
			}
		}
	}
}

func destroyWarm(box runtime.Sandbox) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = box.Destroy(ctx)
}

func (r *Manager) eligible(spec runtime.SandboxSpec) bool {
	return r.settings.Enabled && r.settings.PerImage > 0 && r.settings.MemoryBudgetMB >= warmMemoryMB && spec.Image.Reference != "" && len(spec.SnapshotLayers) == 0 &&
		(spec.NetworkMode == "" || spec.NetworkMode == "none") &&
		(spec.NetworkPolicy.Mode == "" || spec.NetworkPolicy.Mode == "none") && len(spec.NetworkPolicy.Allow) == 0
}

func warmSpec(spec runtime.SandboxSpec) runtime.SandboxSpec {
	spec.Limits = runtime.Limits{CPUMillicores: warmCPUMilli, MemoryMB: warmMemoryMB, PIDs: warmPIDs}
	spec.Env = nil
	spec.NetworkMode = "none"
	spec.NetworkPolicy = runtime.NetworkPolicy{Mode: "none"}
	spec.SnapshotLayers = nil
	return spec
}

func imageKey(image runtime.ImageInfo) string {
	if image.Digest != "" {
		return image.Digest
	}
	return image.Reference
}

// PoolReady reports currently idle sandboxes by bounded image identifier.
func (r *Manager) PoolReady() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]int, len(r.items))
	for key, items := range r.items {
		image := r.demand[key].Image.Reference
		if image == "" {
			image = key
		}
		result[image] += len(items)
	}
	return result
}

// PoolMemoryMB reports memory budget currently reserved by idle and creating entries.
func (r *Manager) PoolMemoryMB() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.usedMB
}

// Close cancels refill and destroys all idle warm sandboxes.
func (r *Manager) Close(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		r.cancel()
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}
	r.mu.Lock()
	items := r.items
	r.items = map[string][]item{}
	r.usedMB = 0
	r.mu.Unlock()
	var firstErr error
	for _, group := range items {
		for _, value := range group {
			if err := value.box.Destroy(ctx); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("destroy warm sandbox: %w", err)
			}
		}
	}
	return firstErr
}
