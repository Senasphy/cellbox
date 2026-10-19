package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

// CreateSession writes a creating session before runtime side effects begin.
func (s *Store) CreateSession(ctx context.Context, v store.SessionRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session creation: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,owner_key_id,image_ref,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,parent_snapshot_id,failure_reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.OwnerKeyID, v.ImageReference, v.ImageDigest, v.RuntimeID, v.State, v.LimitsJSON, v.NetworkPolicyJSON, v.EnvJSON, v.LabelsJSON, stamp(v.CreatedAt), stamp(v.LastActivityAt), stamp(v.ExpiresAt), nullText(v.ParentSnapshotID), v.FailureReason); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	if err = insertAudit(ctx, tx, v.ID, v.OwnerKeyID, "session.create", map[string]string{"state": v.State}, v.CreatedAt); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit session creation: %w", err)
	}
	return nil
}

// UpdateSession persists a session lifecycle transition and runtime metadata.
func (s *Store) UpdateSession(ctx context.Context, v store.SessionRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session update: %w", err)
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM sessions WHERE id=?`, v.ID).Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return fmt.Errorf("read current session state: %w", err)
	}
	if !validSessionTransition(current, v.State) {
		return fmt.Errorf("invalid session state transition %q -> %q", current, v.State)
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET image_digest=?,runtime_id=?,state=?,limits_json=?,network_policy_json=?,env_json=?,labels_json=?,last_activity_at=?,expires_at=?,failure_reason=? WHERE id=? AND state=?`, v.ImageDigest, v.RuntimeID, v.State, v.LimitsJSON, v.NetworkPolicyJSON, v.EnvJSON, v.LabelsJSON, stamp(v.LastActivityAt), stamp(v.ExpiresAt), v.FailureReason, v.ID, current)
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
	if current != v.State {
		detail := map[string]string{"from": current, "to": v.State}
		if err = insertAudit(ctx, tx, v.ID, v.OwnerKeyID, "session."+v.State, detail, v.LastActivityAt); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit session update: %w", err)
	}
	return nil
}

func validSessionTransition(from, to string) bool {
	if from == to {
		return from == "creating" || from == "ready" || from == "stopping" || from == "stopped" || from == "expired" || from == "failed" || from == "lost"
	}
	switch from {
	case "creating":
		return to == "ready" || to == "failed" || to == "stopping" || to == "lost"
	case "ready":
		return to == "stopping" || to == "failed" || to == "expired" || to == "lost"
	case "stopping":
		return to == "stopped" || to == "expired" || to == "lost"
	default:
		return false
	}
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

// PageSessions returns at most limit sessions before cursor, newest first.
func (s *Store) PageSessions(ctx context.Context, owner string, admin bool, limit int, cursor *store.ListCursor) ([]store.SessionRecord, bool, error) {
	if limit < 1 {
		return nil, false, fmt.Errorf("page sessions: limit must be positive")
	}
	query := `SELECT id,owner_key_id,image_ref,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,COALESCE(parent_snapshot_id,''),failure_reason FROM sessions`
	conditions := []string{}
	args := []any{}
	if !admin {
		conditions = append(conditions, `owner_key_id=?`)
		args = append(args, owner)
	}
	if cursor != nil {
		conditions = append(conditions, `(created_at < ? OR (created_at = ? AND id < ?))`)
		createdAt := stamp(cursor.Timestamp)
		args = append(args, createdAt, createdAt, cursor.ID)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("page sessions: %w", err)
	}
	defer rows.Close()
	items := []store.SessionRecord{}
	for rows.Next() {
		item, scanErr := scanSession(rows)
		if scanErr != nil {
			return nil, false, fmt.Errorf("scan session page: %w", scanErr)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate session page: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
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
