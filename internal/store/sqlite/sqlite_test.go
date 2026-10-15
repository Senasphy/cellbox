package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

func TestOpenMigratesEmptyDatabaseAndIsIdempotent(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "cellbox.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 3 {
		t.Fatalf("migration count=%d", n)
	}
}

func TestSessionsPersistLifecycleAndScope(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	v := store.SessionRecord{ID: "ses_a", OwnerKeyID: "key_a", ImageReference: "python:3.12", State: "creating", LimitsJSON: []byte(`{}`), NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if e = s.CreateSession(context.Background(), v); e != nil {
		t.Fatal(e)
	}
	v.State = "ready"
	v.RuntimeID = "container"
	if e = s.UpdateSession(context.Background(), v); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetSession(context.Background(), v.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "ready" || got.RuntimeID != "container" {
		t.Fatalf("session=%+v", got)
	}
	owner, e := s.ListSessions(context.Background(), "key_a", false)
	if e != nil || len(owner) != 1 {
		t.Fatalf("owner list=%d err=%v", len(owner), e)
	}
	other, e := s.ListSessions(context.Background(), "key_b", false)
	if e != nil || len(other) != 0 {
		t.Fatalf("other list=%d err=%v", len(other), e)
	}
	all, e := s.ListSessions(context.Background(), "", true)
	if e != nil || len(all) != 1 {
		t.Fatalf("admin list=%d err=%v", len(all), e)
	}
}

func TestKeysetPaginationForSessionsAndExecs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "pagination.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	session := store.SessionRecord{ID: "ses_owner", OwnerKeyID: "key_owner", ImageReference: "image", State: "ready", LimitsJSON: []byte(`{}`), NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if err = s.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		other := session
		other.ID = fmt.Sprintf("ses_%d", i)
		if err = s.CreateSession(ctx, other); err != nil {
			t.Fatal(err)
		}
		execution := store.ExecRecord{ID: fmt.Sprintf("exe_%d", i), SessionID: session.ID, ArgvJSON: []byte(`[]`), EnvJSON: []byte(`{}`), State: "exited", StartedAt: now}
		if err = s.CreateExec(ctx, execution); err != nil {
			t.Fatal(err)
		}
	}

	var sessionIDs []string
	var sessionCursor *store.ListCursor
	for {
		page, more, pageErr := s.PageSessions(ctx, "key_owner", false, 2, sessionCursor)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		for _, item := range page {
			sessionIDs = append(sessionIDs, item.ID)
		}
		if !more {
			break
		}
		last := page[len(page)-1]
		sessionCursor = &store.ListCursor{Timestamp: last.CreatedAt, ID: last.ID}
	}
	if len(sessionIDs) != 6 {
		t.Fatalf("paginated sessions=%v", sessionIDs)
	}
	for i := 1; i < len(sessionIDs); i++ {
		if sessionIDs[i-1] <= sessionIDs[i] {
			t.Fatalf("sessions are not in stable descending order: %v", sessionIDs)
		}
	}

	var execIDs []string
	var execCursor *store.ListCursor
	for {
		page, more, pageErr := s.PageExecs(ctx, session.ID, 2, execCursor)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		for _, item := range page {
			execIDs = append(execIDs, item.ID)
		}
		if !more {
			break
		}
		last := page[len(page)-1]
		execCursor = &store.ListCursor{Timestamp: last.StartedAt, ID: last.ID}
	}
	if len(execIDs) != 5 {
		t.Fatalf("paginated execs=%v", execIDs)
	}
	for i := 1; i < len(execIDs); i++ {
		if execIDs[i-1] >= execIDs[i] {
			t.Fatalf("execs are not in stable ascending order: %v", execIDs)
		}
	}
}

func TestExecOutputIsStoredBeforeItCanBeResumed(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "execs.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	session := store.SessionRecord{ID: "ses_x", OwnerKeyID: "key_x", ImageReference: "image", State: "ready", LimitsJSON: []byte(`{}`), NetworkPolicyJSON: []byte(`{}`), EnvJSON: []byte(`{}`), LabelsJSON: []byte(`{}`), CreatedAt: now, LastActivityAt: now, ExpiresAt: now.Add(time.Hour)}
	if e = s.CreateSession(context.Background(), session); e != nil {
		t.Fatal(e)
	}
	exec := store.ExecRecord{ID: "exe_x", SessionID: session.ID, ArgvJSON: []byte(`["echo"]`), EnvJSON: []byte(`{}`), State: "running", StartedAt: now}
	if e = s.CreateExec(context.Background(), exec); e != nil {
		t.Fatal(e)
	}
	first, e := s.AppendExecOutput(context.Background(), exec.ID, "stdout", []byte("one"), now)
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.AppendExecOutput(context.Background(), exec.ID, "stderr", []byte("two"), now.Add(time.Millisecond))
	if e != nil {
		t.Fatal(e)
	}
	if first.Offset != 0 || second.Offset != 3 {
		t.Fatalf("offsets=%d,%d", first.Offset, second.Offset)
	}
	items, e := s.ListExecOutput(context.Background(), exec.ID, 3)
	if e != nil {
		t.Fatal(e)
	}
	if len(items) != 1 || string(items[0].Data) != "two" || items[0].Offset != 3 {
		t.Fatalf("resumed output=%+v", items)
	}
	exit := 7
	ended := time.Now().UTC()
	exec.State = "exited"
	exec.ExitCode = &exit
	exec.EndedAt = &ended
	exec.OutputBytes = 6
	if e = s.UpdateExec(context.Background(), exec); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetExec(context.Background(), exec.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "exited" || got.ExitCode == nil || *got.ExitCode != 7 || got.OutputBytes != 6 {
		t.Fatalf("exec=%+v", got)
	}
}

func TestKeyLifecycleStoresOnlyDigest(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "keys.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	key := KeyRecord{ID: "key_a", Name: "admin", Prefix: "ABCDEFGH", Hash: []byte("digest"), Scope: "admin", CreatedAt: time.Now().UTC()}
	if e = s.CreateKey(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	found, e := s.FindKeyByPrefix(context.Background(), key.Prefix)
	if e != nil {
		t.Fatal(e)
	}
	if string(found.Hash) != "digest" {
		t.Fatalf("hash=%q", found.Hash)
	}
	items, e := s.ListKeys(context.Background())
	if e != nil || len(items) != 1 {
		t.Fatalf("keys=%+v err=%v", items, e)
	}
	if e = s.RevokeKey(context.Background(), key.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.FindKeyByPrefix(context.Background(), key.Prefix); e == nil {
		t.Fatal("revoked key remained active")
	}
}
