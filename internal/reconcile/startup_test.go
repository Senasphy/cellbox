package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

type fakeBackend struct {
	calls int
	err   error
}

func (f *fakeBackend) Reconcile(context.Context) error { f.calls++; return f.err }

type fakeSessions struct{ items []store.SessionRecord }

func (f *fakeSessions) ListSessions(context.Context, string, bool) ([]store.SessionRecord, error) {
	return append([]store.SessionRecord(nil), f.items...), nil
}
func (f *fakeSessions) UpdateSession(_ context.Context, item store.SessionRecord) error {
	for i := range f.items {
		if f.items[i].ID == item.ID {
			f.items[i] = item
			return nil
		}
	}
	return errors.New("session not found")
}

func TestStartupCleansBackendThenMarksActiveSessionsLost(t *testing.T) {
	backend := &fakeBackend{}
	sessions := &fakeSessions{items: []store.SessionRecord{{ID: "ses_a", State: "ready"}, {ID: "ses_b", State: "creating"}, {ID: "ses_c", State: "stopped"}}}
	if err := Startup(context.Background(), backend, sessions); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 1 {
		t.Fatalf("backend reconcile calls=%d", backend.calls)
	}
	for _, item := range sessions.items[:2] {
		if item.State != "lost" || item.FailureReason == "" || item.LastActivityAt.Before(time.Now().Add(-time.Minute)) {
			t.Fatalf("session was not marked lost: %+v", item)
		}
	}
	if sessions.items[2].State != "stopped" {
		t.Fatalf("terminal session changed: %+v", sessions.items[2])
	}
}

func TestStartupStopsWhenRuntimeReconcileFails(t *testing.T) {
	backend := &fakeBackend{err: errors.New("docker unavailable")}
	if err := Startup(context.Background(), backend, &fakeSessions{}); err == nil {
		t.Fatal("expected runtime reconciliation error")
	}
}
