// Package postgres implements the cluster-mode PostgreSQL Store using pgx.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senasphy/cellbox/internal/store"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store implements the shared persistence contracts against PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects a pgx connection pool to dsn and applies forward-only migrations.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres DSN must not be empty")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres DSN: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// NewFromPool wraps an existing pgxpool.Pool.
func NewFromPool(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	if s.pool == nil {
		return errors.New("postgres pool is nil")
	}
	return s.pool.Ping(ctx)
}

// Close releases the underlying connection pool.
func (s *Store) Close() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

// Migrate applies embedded SQL migrations exactly once.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read postgres migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var exists int
		err := s.pool.QueryRow(ctx, `SELECT 1 FROM schema_migrations WHERE version=$1`, name).Scan(&exists)
		if err == nil {
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		raw, err := fs.ReadFile(migrationFiles, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(raw)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES($1, $2)`, name, time.Now().UTC()); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func jsonOrDefault(raw []byte, fallback string) []byte {
	if len(raw) == 0 {
		return []byte(fallback)
	}
	return raw
}

var (
	_ store.Store           = (*Store)(nil)
	_ store.SessionStore    = (*Store)(nil)
	_ store.ExecStore       = (*Store)(nil)
	_ store.IdempotencyStore = (*Store)(nil)
	_ store.AuditEventStore = (*Store)(nil)
	_ store.EgressLogStore  = (*Store)(nil)
	_ store.SnapshotStore   = (*Store)(nil)
)
