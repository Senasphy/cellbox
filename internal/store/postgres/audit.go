package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/store"
)

// RecordLimitBreach appends a session limit breach audit event.
func (s *Store) RecordLimitBreach(ctx context.Context, sessionID, keyID, failureReason string, detail any, at time.Time) error {
	eventID, err := id.New("evt")
	if err != nil {
		return fmt.Errorf("generate audit event id: %w", err)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal limit breach detail: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO audit_events(id,session_id,key_id,type,timestamp,detail) VALUES($1,$2,$3,$4,$5,$6)`,
		eventID, sessionID, keyID, "session.limit_breach", at.UTC(), raw); err != nil {
		return fmt.Errorf("insert limit breach audit event: %w", err)
	}
	return nil
}

// ListAuditEvents returns recent audit events for one session.
func (s *Store) ListAuditEvents(ctx context.Context, sessionID string, limit int) ([]store.AuditEventRecord, error) {
	items, _, err := s.PageAuditEvents(ctx, sessionID, limit, nil)
	return items, err
}

// RecentLimitBreaches returns recent limit breach events visible to the caller.
func (s *Store) RecentLimitBreaches(ctx context.Context, ownerKeyID string, admin bool, limit int) ([]store.AuditEventRecord, error) {
	limit = normalizeLimit(limit)
	query := `SELECT id,session_id,key_id,type,timestamp,detail FROM audit_events WHERE type='session.limit_breach'`
	var args []any
	if !admin {
		query += ` AND key_id=$1 ORDER BY timestamp DESC, id DESC LIMIT $2`
		args = append(args, ownerKeyID, limit)
	} else {
		query += ` ORDER BY timestamp DESC, id DESC LIMIT $1`
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list recent limit breaches: %w", err)
	}
	defer rows.Close()
	out := []store.AuditEventRecord{}
	for rows.Next() {
		var rec store.AuditEventRecord
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.KeyID, &rec.Type, &rec.Timestamp, &rec.Detail); err != nil {
			return nil, fmt.Errorf("scan limit breach: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// PageAuditEvents returns keyset-paginated audit events for one session.
func (s *Store) PageAuditEvents(ctx context.Context, sessionID string, limit int, cursor *store.ListCursor) ([]store.AuditEventRecord, bool, error) {
	limit = normalizeLimit(limit)
	query := `SELECT id,session_id,key_id,type,timestamp,detail FROM audit_events WHERE session_id=$1`
	args := []any{sessionID}
	if cursor != nil {
		query += ` AND (timestamp < $2 OR (timestamp = $3 AND id < $4)) ORDER BY timestamp DESC, id DESC LIMIT $5`
		args = append(args, cursor.Timestamp.UTC(), cursor.Timestamp.UTC(), cursor.ID, limit+1)
	} else {
		query += ` ORDER BY timestamp DESC, id DESC LIMIT $2`
		args = append(args, limit+1)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("page audit events: %w", err)
	}
	defer rows.Close()
	out := make([]store.AuditEventRecord, 0, limit+1)
	for rows.Next() {
		var rec store.AuditEventRecord
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.KeyID, &rec.Type, &rec.Timestamp, &rec.Detail); err != nil {
			return nil, false, fmt.Errorf("scan audit event: %w", err)
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

// RecordEgressLog persists an egress decision entry.
func (s *Store) RecordEgressLog(ctx context.Context, sessionID, keyID string, entry store.EgressLogEntry) error {
	entryID, err := id.New("egr")
	if err != nil {
		return fmt.Errorf("generate egress log id: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO egress_log(id,session_id,key_id,timestamp,host,port,resolved_ip,verdict,reason,bytes_up,bytes_down,duration_ms) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		entryID, sessionID, keyID, entry.Timestamp.UTC(), entry.Host, entry.Port, entry.Resolved, entry.Verdict, entry.Reason, entry.BytesUp, entry.BytesDown, entry.Duration,
	); err != nil {
		return fmt.Errorf("insert egress log: %w", err)
	}
	return nil
}

// PageEgressEvents returns keyset-paginated egress log entries for one session.
func (s *Store) PageEgressEvents(ctx context.Context, sessionID string, limit int, cursor *store.ListCursor) ([]store.EgressLogRecord, bool, error) {
	limit = normalizeLimit(limit)
	query := `SELECT id,session_id,key_id,timestamp,host,port,resolved_ip,verdict,reason,bytes_up,bytes_down,duration_ms FROM egress_log WHERE session_id=$1`
	args := []any{sessionID}
	if cursor != nil {
		query += ` AND (timestamp < $2 OR (timestamp = $3 AND id < $4)) ORDER BY timestamp DESC, id DESC LIMIT $5`
		args = append(args, cursor.Timestamp.UTC(), cursor.Timestamp.UTC(), cursor.ID, limit+1)
	} else {
		query += ` ORDER BY timestamp DESC, id DESC LIMIT $2`
		args = append(args, limit+1)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("page egress log: %w", err)
	}
	defer rows.Close()
	out := make([]store.EgressLogRecord, 0, limit+1)
	for rows.Next() {
		var rec store.EgressLogRecord
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.KeyID, &rec.Timestamp, &rec.Host, &rec.Port, &rec.Resolved, &rec.Verdict, &rec.Reason, &rec.BytesUp, &rec.BytesDown, &rec.Duration); err != nil {
			return nil, false, fmt.Errorf("scan egress log: %w", err)
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

// PruneAuditEvents deletes audit events older than before.
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audit_events WHERE timestamp < $1`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("prune audit events: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PruneExecOutput deletes exec output for execs that ended before the cutoff.
func (s *Store) PruneExecOutput(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM exec_output WHERE exec_id IN (SELECT id FROM execs WHERE ended_at IS NOT NULL AND ended_at < $1)`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("prune exec output: %w", err)
	}
	return tag.RowsAffected(), nil
}
