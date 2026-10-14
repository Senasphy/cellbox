package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

// KeyRecord aliases the shared stored key metadata type.
type KeyRecord = store.KeyRecord

// CreateKey stores key metadata and its digest, never the secret itself.
func (s *Store) CreateKey(ctx context.Context, k KeyRecord) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO api_keys(id,name,prefix,secret_sha256,scope,created_at) VALUES(?,?,?,?,?,?)`, k.ID, k.Name, k.Prefix, k.Hash, k.Scope, k.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("insert API key: %w", err)
	}
	return nil
}

// ListKeys returns key metadata in creation order.
func (s *Store) ListKeys(ctx context.Context) ([]KeyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,prefix,secret_sha256,scope,created_at,last_used_at,revoked_at FROM api_keys ORDER BY created_at,id`)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()
	keys := []KeyRecord{}
	for rows.Next() {
		var k KeyRecord
		var created string
		var last, revoked sql.NullString
		if err = rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Hash, &k.Scope, &created, &last, &revoked); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}
		if k.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, fmt.Errorf("parse API key creation time: %w", err)
		}
		if last.Valid {
			t, e := time.Parse(time.RFC3339Nano, last.String)
			if e != nil {
				return nil, fmt.Errorf("parse API key last-used time: %w", e)
			}
			k.LastUsedAt = &t
		}
		if revoked.Valid {
			t, e := time.Parse(time.RFC3339Nano, revoked.String)
			if e != nil {
				return nil, fmt.Errorf("parse API key revocation time: %w", e)
			}
			k.RevokedAt = &t
		}
		keys = append(keys, k)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API keys: %w", err)
	}
	return keys, nil
}

// FindKeyByPrefix returns an active key matching its public prefix.
func (s *Store) FindKeyByPrefix(ctx context.Context, prefix string) (KeyRecord, error) {
	var k KeyRecord
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,prefix,secret_sha256,scope,created_at FROM api_keys WHERE prefix=? AND revoked_at IS NULL LIMIT 1`, prefix).Scan(&k.ID, &k.Name, &k.Prefix, &k.Hash, &k.Scope, &created)
	if err != nil {
		return k, fmt.Errorf("find API key: %w", err)
	}
	if k.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return k, fmt.Errorf("parse API key creation time: %w", err)
	}
	return k, nil
}

// RevokeKey marks an API key revoked and is idempotent for existing keys.
func (s *Store) RevokeKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET revoked_at=COALESCE(revoked_at,?) WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check revoked API key: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkKeyUsed stores the most recent successful authentication time.
func (s *Store) MarkKeyUsed(ctx context.Context, id string, usedAt time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=? AND revoked_at IS NULL`, usedAt.UTC().Format(time.RFC3339Nano), id); err != nil {
		return fmt.Errorf("mark API key used: %w", err)
	}
	return nil
}
