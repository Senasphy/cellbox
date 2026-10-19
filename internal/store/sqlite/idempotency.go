package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

func (s *Store) GetIdempotency(ctx context.Context, keyHash []byte) (store.IdempotencyRecord, error) {
	var v store.IdempotencyRecord
	var created string
	cutoff := stamp(time.Now().UTC().Add(-24 * time.Hour))
	err := s.db.QueryRowContext(ctx, `SELECT key_hash,request_hash,status_code,headers_json,response_body,created_at FROM idempotency WHERE key_hash=? AND created_at >= ?`, keyHash, cutoff).Scan(&v.KeyHash, &v.RequestHash, &v.StatusCode, &v.HeadersJSON, &v.Body, &created)
	if err != nil {
		return v, err
	}
	v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return v, fmt.Errorf("parse idempotency timestamp: %w", err)
	}
	return v, nil
}

func (s *Store) SaveIdempotency(ctx context.Context, v store.IdempotencyRecord) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM idempotency WHERE created_at < ?`, stamp(v.CreatedAt.UTC().Add(-24*time.Hour))); err != nil {
		return fmt.Errorf("prune expired idempotency responses: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO idempotency(key_hash,request_hash,status_code,headers_json,response_body,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(key_hash) DO UPDATE SET request_hash=excluded.request_hash,status_code=excluded.status_code,headers_json=excluded.headers_json,response_body=excluded.response_body,created_at=excluded.created_at`, v.KeyHash, v.RequestHash, v.StatusCode, v.HeadersJSON, v.Body, stamp(v.CreatedAt))
	if err != nil {
		return fmt.Errorf("save idempotency response: %w", err)
	}
	return nil
}
