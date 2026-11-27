package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/store"
)

// CreateSession inserts an initial session record and its creation audit event.
func (s *Store) CreateSession(ctx context.Context, v store.SessionRecord) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin session create: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx,
		`INSERT INTO sessions(id,owner_key_id,image_reference,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,parent_snapshot_id,failure_reason,create_source)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		v.ID, v.OwnerKeyID, v.ImageReference, v.ImageDigest, v.RuntimeID, v.State,
		jsonOrDefault(v.LimitsJSON, `{}`), jsonOrDefault(v.NetworkPolicyJSON, `{}`), jsonOrDefault(v.EnvJSON, `{}`), jsonOrDefault(v.LabelsJSON, `{}`),
		v.CreatedAt.UTC(), v.LastActivityAt.UTC(), v.ExpiresAt.UTC(), v.ParentSnapshotID, v.FailureReason, v.CreateSource,
	); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	if err = insertAuditTx(ctx, tx, v.ID, v.OwnerKeyID, "session."+v.State, map[string]string{"state": v.State}, v.CreatedAt); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit session create: %w", err)
	}
	return nil
}

// UpdateSession persists a validated session lifecycle transition and audit event.
func (s *Store) UpdateSession(ctx context.Context, v store.SessionRecord) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin session update: %w", err)
	}
	defer tx.Rollback(ctx)
	var current string
	err = tx.QueryRow(ctx, `SELECT state FROM sessions WHERE id=$1 FOR UPDATE`, v.ID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return fmt.Errorf("read current session state: %w", err)
	}
	if !validSessionTransition(current, v.State) {
		return fmt.Errorf("invalid session state transition %q -> %q", current, v.State)
	}
	tag, err := tx.Exec(ctx,
		`UPDATE sessions SET image_digest=$1,runtime_id=$2,state=$3,limits_json=$4,network_policy_json=$5,env_json=$6,labels_json=$7,last_activity_at=$8,expires_at=$9,failure_reason=$10 WHERE id=$11 AND state=$12`,
		v.ImageDigest, v.RuntimeID, v.State,
		jsonOrDefault(v.LimitsJSON, `{}`), jsonOrDefault(v.NetworkPolicyJSON, `{}`), jsonOrDefault(v.EnvJSON, `{}`), jsonOrDefault(v.LabelsJSON, `{}`),
		v.LastActivityAt.UTC(), v.ExpiresAt.UTC(), v.FailureReason, v.ID, current,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	if current != v.State {
		detail := map[string]string{"from": current, "to": v.State}
		if current == "creating" && v.State == "ready" && (v.CreateSource == "warm" || v.CreateSource == "cold") {
			detail["source"] = v.CreateSource
		}
		if err = insertAuditTx(ctx, tx, v.ID, v.OwnerKeyID, "session."+v.State, detail, v.LastActivityAt); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
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

// GetSession loads one session record by ID.
func (s *Store) GetSession(ctx context.Context, id string) (store.SessionRecord, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,owner_key_id,image_reference,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,parent_snapshot_id,failure_reason,create_source FROM sessions WHERE id=$1`, id)
	rec, err := scanSessionRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, sql.ErrNoRows
	}
	return rec, err
}

// ListSessions returns sessions visible to owner (or all if admin is true).
func (s *Store) ListSessions(ctx context.Context, owner string, admin bool) ([]store.SessionRecord, error) {
	query := `SELECT id,owner_key_id,image_reference,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,parent_snapshot_id,failure_reason,create_source FROM sessions`
	var args []any
	if !admin {
		query += ` WHERE owner_key_id=$1`
		args = append(args, owner)
	}
	query += ` ORDER BY created_at DESC, id DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	out := []store.SessionRecord{}
	for rows.Next() {
		rec, err := scanSessionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// PageSessions returns one keyset-paginated slice of sessions.
func (s *Store) PageSessions(ctx context.Context, owner string, admin bool, limit int, cursor *store.ListCursor) ([]store.SessionRecord, bool, error) {
	limit = normalizeLimit(limit)
	query := `SELECT id,owner_key_id,image_reference,image_digest,runtime_id,state,limits_json,network_policy_json,env_json,labels_json,created_at,last_activity_at,expires_at,parent_snapshot_id,failure_reason,create_source FROM sessions`
	var args []any
	idx := 1
	addClause := func(clause string, val ...any) {
		if len(args) == 0 {
			query += " WHERE " + clause
		} else {
			query += " AND " + clause
		}
		args = append(args, val...)
	}
	if !admin {
		addClause(fmt.Sprintf("owner_key_id=$%d", idx), owner)
		idx++
	}
	if cursor != nil {
		addClause(fmt.Sprintf("(created_at < $%d OR (created_at = $%d AND id < $%d))", idx, idx+1, idx+2), cursor.Timestamp.UTC(), cursor.Timestamp.UTC(), cursor.ID)
		idx += 3
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", idx)
	args = append(args, limit+1)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("page sessions: %w", err)
	}
	defer rows.Close()
	out := make([]store.SessionRecord, 0, limit+1)
	for rows.Next() {
		rec, err := scanSessionRow(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSessionRow(row rowScanner) (store.SessionRecord, error) {
	var v store.SessionRecord
	err := row.Scan(
		&v.ID, &v.OwnerKeyID, &v.ImageReference, &v.ImageDigest, &v.RuntimeID, &v.State,
		&v.LimitsJSON, &v.NetworkPolicyJSON, &v.EnvJSON, &v.LabelsJSON,
		&v.CreatedAt, &v.LastActivityAt, &v.ExpiresAt, &v.ParentSnapshotID, &v.FailureReason, &v.CreateSource,
	)
	if err != nil {
		return store.SessionRecord{}, err
	}
	return v, nil
}

func insertAuditTx(ctx context.Context, tx pgx.Tx, sessionID, keyID, eventType string, detail any, at time.Time) error {
	eventID, err := id.New("evt")
	if err != nil {
		return fmt.Errorf("generate audit event id: %w", err)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal audit detail: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,session_id,key_id,type,timestamp,detail) VALUES($1,$2,$3,$4,$5,$6)`,
		eventID, sessionID, keyID, eventType, at.UTC(), raw); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}
