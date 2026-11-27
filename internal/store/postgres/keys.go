package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/senasphy/cellbox/internal/store"
)

// CreateKey stores API key metadata and its secret digest.
func (s *Store) CreateKey(ctx context.Context, k store.KeyRecord) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO api_keys(id,name,prefix,secret_sha256,scope,quotas_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		k.ID, k.Name, k.Prefix, k.Hash, k.Scope, jsonOrDefault(k.QuotasJSON, `{}`), k.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert API key: %w", err)
	}
	return nil
}

// ListKeys returns API key metadata in creation order.
func (s *Store) ListKeys(ctx context.Context) ([]store.KeyRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,prefix,secret_sha256,scope,quotas_json,created_at,last_used_at,revoked_at FROM api_keys ORDER BY created_at,id`)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()
	keys := []store.KeyRecord{}
	for rows.Next() {
		var k store.KeyRecord
		var last, revoked *time.Time
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Hash, &k.Scope, &k.QuotasJSON, &k.CreatedAt, &last, &revoked); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}
		k.LastUsedAt = last
		k.RevokedAt = revoked
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API keys: %w", err)
	}
	return keys, nil
}

// FindKeyByPrefix returns an active key matching its public prefix.
func (s *Store) FindKeyByPrefix(ctx context.Context, prefix string) (store.KeyRecord, error) {
	var k store.KeyRecord
	err := s.pool.QueryRow(ctx, `SELECT id,name,prefix,secret_sha256,scope,quotas_json,created_at FROM api_keys WHERE prefix=$1 AND revoked_at IS NULL LIMIT 1`, prefix).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.Hash, &k.Scope, &k.QuotasJSON, &k.CreatedAt)
	if err != nil {
		return k, fmt.Errorf("find API key: %w", err)
	}
	return k, nil
}

// RevokeKey marks an API key revoked and is idempotent for existing keys.
func (s *Store) RevokeKey(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at=COALESCE(revoked_at,$1) WHERE id=$2`, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkKeyUsed updates the most recent authentication timestamp.
func (s *Store) MarkKeyUsed(ctx context.Context, id string, usedAt time.Time) error {
	if _, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=$1 WHERE id=$2 AND revoked_at IS NULL`, usedAt.UTC(), id); err != nil {
		return fmt.Errorf("mark API key used: %w", err)
	}
	return nil
}

// UpdateKeyQuotas updates the quotas JSON for an active API key.
func (s *Store) UpdateKeyQuotas(ctx context.Context, id string, quotasJSON []byte) (store.KeyRecord, error) {
	var k store.KeyRecord
	var last, revoked *time.Time
	err := s.pool.QueryRow(ctx,
		`UPDATE api_keys SET quotas_json=$1 WHERE id=$2 AND revoked_at IS NULL RETURNING id,name,prefix,secret_sha256,scope,quotas_json,created_at,last_used_at,revoked_at`,
		jsonOrDefault(quotasJSON, `{}`), id,
	).Scan(&k.ID, &k.Name, &k.Prefix, &k.Hash, &k.Scope, &k.QuotasJSON, &k.CreatedAt, &last, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, sql.ErrNoRows
	}
	if err != nil {
		return k, fmt.Errorf("update API key quotas: %w", err)
	}
	k.LastUsedAt = last
	k.RevokedAt = revoked
	return k, nil
}
