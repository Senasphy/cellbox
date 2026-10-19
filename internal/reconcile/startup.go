// Package reconcile restores persisted lifecycle consistency during startup.
package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

type runtimeReconciler interface {
	Reconcile(context.Context) error
}

type sessionStore interface {
	ListSessions(context.Context, string, bool) ([]store.SessionRecord, error)
	UpdateSession(context.Context, store.SessionRecord) error
}

// Startup removes backend orphans and marks sessions that lost their in-memory
// actor during a process restart as lost.
func Startup(ctx context.Context, backend runtimeReconciler, sessions sessionStore) error {
	if backend != nil {
		if err := backend.Reconcile(ctx); err != nil {
			return fmt.Errorf("reconcile runtime: %w", err)
		}
	}
	if sessions == nil {
		return nil
	}
	items, err := sessions.ListSessions(ctx, "", true)
	if err != nil {
		return fmt.Errorf("list sessions for startup reconciliation: %w", err)
	}
	now := time.Now().UTC()
	for _, item := range items {
		switch item.State {
		case "creating", "ready", "stopping":
			item.State = "lost"
			item.FailureReason = "service restarted while session was active"
			item.LastActivityAt = now
			if err := sessions.UpdateSession(ctx, item); err != nil {
				return fmt.Errorf("mark session %s lost during startup reconciliation: %w", item.ID, err)
			}
		}
	}
	return nil
}
