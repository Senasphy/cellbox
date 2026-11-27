package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/senasphy/cellbox/internal/store"
)

// GetIdempotency retrieves a previously saved idempotent response.
func (s *Store) GetIdempotency(ctx context.Context, keyHash []byte) (store.IdempotencyRecord, error) {
	var v store.IdempotencyRecord
	err := s.pool.QueryRow(ctx, `SELECT key_hash,request_hash,status_code,headers_json,body,created_at FROM idempotency WHERE key_hash=$1`, keyHash).
		Scan(&v.KeyHash, &v.RequestHash, &v.StatusCode, &v.HeadersJSON, &v.Body, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, sql.ErrNoRows
	}
	if err != nil {
		return v, fmt.Errorf("get idempotency record: %w", err)
	}
	return v, nil
}

// SaveIdempotency persists a completed idempotent response.
func (s *Store) SaveIdempotency(ctx context.Context, v store.IdempotencyRecord) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO idempotency(key_hash,request_hash,status_code,headers_json,body,created_at) VALUES($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (key_hash) DO UPDATE SET request_hash=EXCLUDED.request_hash,status_code=EXCLUDED.status_code,headers_json=EXCLUDED.headers_json,body=EXCLUDED.body,created_at=EXCLUDED.created_at`,
		v.KeyHash, v.RequestHash, v.StatusCode, jsonOrDefault(v.HeadersJSON, `{}`), v.Body, v.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("save idempotency record: %w", err)
	}
	return nil
}
