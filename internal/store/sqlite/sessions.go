package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

// CreateSession writes a creating session before runtime side effects begin.
func (s *Store) CreateSession(ctx context.Context, v store.SessionRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id,owner_key_id,image_ref,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,parent_snapshot_id,failure_reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.OwnerKeyID, v.ImageReference, v.ImageDigest, v.RuntimeID, v.State, v.LimitsJSON, v.NetworkPolicyJSON, v.EnvJSON, v.LabelsJSON, stamp(v.CreatedAt), stamp(v.LastActivityAt), stamp(v.ExpiresAt), nullText(v.ParentSnapshotID), v.FailureReason)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// UpdateSession persists a session lifecycle transition and runtime metadata.
func (s *Store) UpdateSession(ctx context.Context, v store.SessionRecord) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET image_digest=?,runtime_id=?,state=?,limits_json=?,network_policy_json=?,env_json=?,labels_json=?,last_activity_at=?,expires_at=?,failure_reason=? WHERE id=?`, v.ImageDigest, v.RuntimeID, v.State, v.LimitsJSON, v.NetworkPolicyJSON, v.EnvJSON, v.LabelsJSON, stamp(v.LastActivityAt), stamp(v.ExpiresAt), v.FailureReason, v.ID)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check session update: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSession returns one persisted session.
func (s *Store) GetSession(ctx context.Context, id string) (store.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,owner_key_id,image_ref,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,COALESCE(parent_snapshot_id,''),failure_reason FROM sessions WHERE id=?`, id)
	v, err := scanSession(row)
	if err != nil {
		return v, fmt.Errorf("get session: %w", err)
	}
	return v, nil
}

// ListSessions returns all sessions visible to a key, newest first.
func (s *Store) ListSessions(ctx context.Context, owner string, admin bool) ([]store.SessionRecord, error) {
	query := `SELECT id,owner_key_id,image_ref,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,COALESCE(parent_snapshot_id,''),failure_reason FROM sessions`
	args := []any{}
	if !admin {
		query += ` WHERE owner_key_id=?`
		args = append(args, owner)
	}
	query += ` ORDER BY created_at DESC,id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	result := []store.SessionRecord{}
	for rows.Next() {
		v, e := scanSession(rows)
		if e != nil {
			return nil, fmt.Errorf("scan session: %w", e)
		}
		result = append(result, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return result, nil
}

type scanner interface{ Scan(...any) error }

func scanSession(row scanner) (store.SessionRecord, error) {
	var v store.SessionRecord
	var created, last, expires string
	err := row.Scan(&v.ID, &v.OwnerKeyID, &v.ImageReference, &v.ImageDigest, &v.RuntimeID, &v.State, &v.LimitsJSON, &v.NetworkPolicyJSON, &v.EnvJSON, &v.LabelsJSON, &created, &last, &expires, &v.ParentSnapshotID, &v.FailureReason)
	if err != nil {
		return v, err
	}
	for _, item := range []struct {
		src string
		dst *time.Time
	}{{created, &v.CreatedAt}, {last, &v.LastActivityAt}, {expires, &v.ExpiresAt}} {
		*item.dst, err = time.Parse(time.RFC3339Nano, item.src)
		if err != nil {
			return v, fmt.Errorf("parse session timestamp: %w", err)
		}
	}
	return v, nil
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func nullText(v string) any {
	if v == "" {
		return nil
	}
	return v
}
