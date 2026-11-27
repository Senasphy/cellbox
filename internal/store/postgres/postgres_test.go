package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

func TestOpenRejectsInvalidDSN(t *testing.T) {
	if _, err := Open(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty DSN")
	}
	if _, err := Open(context.Background(), "not a valid dsn ://"); err == nil {
		t.Fatal("expected error for malformed DSN")
	}
}

func TestEmbeddedMigrationContainsClusterTables(t *testing.T) {
	raw, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(raw)
	for _, table := range []string{"api_keys", "sessions", "execs", "exec_output", "idempotency", "audit_events", "egress_log", "snapshots", "cluster_workers", "session_placements"} {
		if !strings.Contains(sqlText, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("migration missing table %s", table)
		}
	}
}

func TestLivePostgresStore(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CELLBOX_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("CELLBOX_TEST_POSTGRES_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_ = s.UpsertWorker(ctx, ClusterWorkerRecord{
		ID:              "wrk_test",
		AdvertiseURL:    "http://127.0.0.1:8081",
		State:           "ready",
		TotalCPUMilli:   4000,
		TotalMemoryMB:   4096,
		TotalPIDs:       1024,
		LastHeartbeatAt: time.Now().UTC(),
	})
	workers, err := s.ListWorkers(ctx)
	if err != nil || len(workers) == 0 {
		t.Fatalf("ListWorkers = %v, %v", workers, err)
	}
	var _ store.Store = s
}
