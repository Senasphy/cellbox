package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
	"github.com/senasphy/cellbox/internal/store"
)

type sandboxRuntime interface {
	SandboxByID(string) runtime.Sandbox
}

const stoppingRetryGrace = 30 * time.Second

// Sweep expires ready sessions whose wall-clock or idle TTL has elapsed.
// The stopping state is persisted before runtime cleanup so a later sweep can
// retry cleanup after a transient backend failure.
func Sweep(ctx context.Context, backend sandboxRuntime, sessions sessionStore, now time.Time) error {
	if sessions == nil {
		return nil
	}
	items, err := sessions.ListSessions(ctx, "", true)
	if err != nil {
		return fmt.Errorf("list sessions for TTL sweep: %w", err)
	}
	for _, item := range items {
		if item.State != "ready" && item.State != "stopping" {
			continue
		}
		if item.State == "ready" && !sessionExpired(item, now) {
			continue
		}
		if item.State == "stopping" && !item.LastActivityAt.IsZero() && now.Sub(item.LastActivityAt) < stoppingRetryGrace {
			continue
		}
		if item.State == "ready" {
			item.State = "stopping"
			if err := sessions.UpdateSession(ctx, item); err != nil {
				return fmt.Errorf("persist stopping state for session %s: %w", item.ID, err)
			}
		}
		if backend != nil && item.RuntimeID != "" {
			if box := backend.SandboxByID(item.RuntimeID); box != nil {
				if err := box.Destroy(ctx); err != nil {
					return fmt.Errorf("destroy expired session %s: %w", item.ID, err)
				}
			}
		}
		item.State = "expired"
		item.LastActivityAt = now.UTC()
		if err := sessions.UpdateSession(ctx, item); err != nil {
			return fmt.Errorf("persist expired state for session %s: %w", item.ID, err)
		}
	}
	return nil
}

func sessionExpired(item store.SessionRecord, now time.Time) bool {
	if !item.ExpiresAt.IsZero() && !now.Before(item.ExpiresAt) {
		return true
	}
	var limits runtime.Limits
	if json.Unmarshal(item.LimitsJSON, &limits) == nil && limits.IdleTTLSeconds > 0 && !item.LastActivityAt.IsZero() {
		return now.Sub(item.LastActivityAt) >= time.Duration(limits.IdleTTLSeconds)*time.Second
	}
	return false
}

// Run periodically applies TTL cleanup until ctx is canceled. Errors are
// reported and retried on the next tick.
func Run(ctx context.Context, backend sandboxRuntime, sessions sessionStore, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := Sweep(ctx, backend, sessions, now); err != nil && report != nil {
				report(err)
			}
		}
	}
}
