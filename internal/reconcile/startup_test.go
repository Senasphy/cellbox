package reconcile

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/runtime"
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

type fakeSandbox struct{ destroyed int }

func (*fakeSandbox) ID() string { return "container" }
func (*fakeSandbox) Exec(context.Context, runtime.ExecRequest) (runtime.ExecHandle, error) {
	return nil, runtime.ErrUnsupported
}
func (*fakeSandbox) OpenTerminal(context.Context, runtime.TerminalRequest) (runtime.Terminal, error) {
	return nil, runtime.ErrUnsupported
}
func (*fakeSandbox) ReadFile(context.Context, string) (io.ReadCloser, runtime.FileInfo, error) {
	return nil, runtime.FileInfo{}, runtime.ErrUnsupported
}
func (*fakeSandbox) WriteFile(context.Context, string, io.Reader, fs.FileMode) error {
	return runtime.ErrUnsupported
}
func (*fakeSandbox) DeleteFile(context.Context, string) error { return runtime.ErrUnsupported }
func (*fakeSandbox) ListDir(context.Context, string) ([]runtime.FileInfo, error) {
	return nil, runtime.ErrUnsupported
}
func (*fakeSandbox) Archive(context.Context, string) (io.ReadCloser, error) {
	return nil, runtime.ErrUnsupported
}
func (*fakeSandbox) Extract(context.Context, string, io.Reader) error { return runtime.ErrUnsupported }
func (*fakeSandbox) Snapshot(context.Context) (runtime.SnapshotInfo, error) {
	return runtime.SnapshotInfo{}, runtime.ErrUnsupported
}
func (*fakeSandbox) UpdateLimits(context.Context, runtime.Limits) error {
	return runtime.ErrUnsupported
}
func (*fakeSandbox) Stats(context.Context) (runtime.Stats, error) {
	return runtime.Stats{}, runtime.ErrUnsupported
}
func (*fakeSandbox) Kill(context.Context) error      { return nil }
func (f *fakeSandbox) Destroy(context.Context) error { f.destroyed++; return nil }

type fakeSandboxRuntime struct{ box fakeSandbox }

func (f *fakeSandboxRuntime) SandboxByID(string) runtime.Sandbox { return &f.box }

func TestSweepExpiresWallClockAndIdleSessions(t *testing.T) {
	now := time.Now().UTC()
	sessions := &fakeSessions{items: []store.SessionRecord{
		{ID: "ses_wall", RuntimeID: "container", State: "ready", ExpiresAt: now.Add(-time.Second), LastActivityAt: now, LimitsJSON: []byte(`{"idle_ttl_s":60}`)},
		{ID: "ses_idle", RuntimeID: "container", State: "ready", ExpiresAt: now.Add(time.Hour), LastActivityAt: now.Add(-61 * time.Second), LimitsJSON: []byte(`{"idle_ttl_s":60}`)},
		{ID: "ses_active", RuntimeID: "container", State: "ready", ExpiresAt: now.Add(time.Hour), LastActivityAt: now, LimitsJSON: []byte(`{"idle_ttl_s":60}`)},
	}}
	backend := &fakeSandboxRuntime{}
	if err := Sweep(context.Background(), backend, sessions, now); err != nil {
		t.Fatal(err)
	}
	if sessions.items[0].State != "expired" || sessions.items[1].State != "expired" || sessions.items[2].State != "ready" {
		t.Fatalf("session states after sweep=%q,%q,%q", sessions.items[0].State, sessions.items[1].State, sessions.items[2].State)
	}
	if backend.box.destroyed != 2 {
		t.Fatalf("destroy calls=%d; want 2", backend.box.destroyed)
	}
}

func TestSweepRetriesStoppingSession(t *testing.T) {
	now := time.Now().UTC()
	sessions := &fakeSessions{items: []store.SessionRecord{{ID: "ses_stopping", RuntimeID: "container", State: "stopping", ExpiresAt: now.Add(time.Hour)}}}
	backend := &fakeSandboxRuntime{}
	if err := Sweep(context.Background(), backend, sessions, now); err != nil {
		t.Fatal(err)
	}
	if sessions.items[0].State != "expired" || backend.box.destroyed != 1 {
		t.Fatalf("session=%+v destroy_calls=%d", sessions.items[0], backend.box.destroyed)
	}
}
