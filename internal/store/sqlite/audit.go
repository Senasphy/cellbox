package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/id"
	"github.com/senasphy/cellbox/internal/store"
)

func insertAudit(ctx context.Context, tx *sql.Tx, sessionID, keyID, eventType string, detail any, at time.Time) error {
	eventID, err := id.New("evt")
	if err != nil {
		return fmt.Errorf("generate audit event id: %w", err)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode audit event detail: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,session_id,key_id,type,timestamp,detail_json) VALUES(?,?,?,?,?,?)`, eventID, nullText(sessionID), keyID, eventType, stamp(at), encoded)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s *Store) ListAuditEvents(ctx context.Context, sessionID string, limit int) ([]store.AuditEventRecord, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("audit event limit must be between 1 and 1000")
	}
	query := `SELECT id,COALESCE(session_id,''),key_id,type,timestamp,detail_json FROM audit_events`
	args := []any{}
	if sessionID != "" {
		query += ` WHERE session_id=?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY timestamp,id LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	items := []store.AuditEventRecord{}
	for rows.Next() {
		var item store.AuditEventRecord
		var ts string
		if err := rows.Scan(&item.ID, &item.SessionID, &item.KeyID, &item.Type, &ts, &item.Detail); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		item.Timestamp, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parse audit event timestamp: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return items, nil
}

func (s *Store) PageAuditEvents(ctx context.Context, sessionID string, limit int, cursor *store.ListCursor) ([]store.AuditEventRecord, bool, error) {
	if limit < 1 {
		return nil, false, fmt.Errorf("audit event limit must be positive")
	}
	query := `SELECT id,COALESCE(session_id,''),key_id,type,timestamp,detail_json FROM audit_events WHERE session_id=?`
	args := []any{sessionID}
	if cursor != nil {
		ts := stamp(cursor.Timestamp)
		query += ` AND (timestamp > ? OR (timestamp = ? AND id > ?))`
		args = append(args, ts, ts, cursor.ID)
	}
	query += ` ORDER BY timestamp,id LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("page audit events: %w", err)
	}
	defer rows.Close()
	items := []store.AuditEventRecord{}
	for rows.Next() {
		var item store.AuditEventRecord
		var ts string
		if err := rows.Scan(&item.ID, &item.SessionID, &item.KeyID, &item.Type, &ts, &item.Detail); err != nil {
			return nil, false, fmt.Errorf("scan audit event page: %w", err)
		}
		item.Timestamp, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, false, fmt.Errorf("parse audit event timestamp: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate audit event page: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}
