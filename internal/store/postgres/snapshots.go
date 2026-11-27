package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/senasphy/cellbox/internal/store"
)

// CreateSnapshot persists a snapshot metadata record.
func (s *Store) CreateSnapshot(ctx context.Context, record store.SnapshotRecord) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO snapshots(id,source_session_id,parent_snapshot_id,runtime_snapshot_id,layer_path,size,flattened,created_at,labels_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		record.ID, record.SourceSessionID, record.ParentSnapshotID, record.RuntimeSnapshotID, record.LayerPath, record.Size, record.Flattened, record.CreatedAt.UTC(), jsonOrDefault(record.LabelsJSON, `{}`),
	); err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	return nil
}

// ListSnapshots returns snapshots for one source session ordered newest-first.
func (s *Store) ListSnapshots(ctx context.Context, sessionID string) ([]store.SnapshotRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,source_session_id,parent_snapshot_id,runtime_snapshot_id,layer_path,size,flattened,created_at,labels_json FROM snapshots WHERE source_session_id=$1 ORDER BY created_at DESC, id DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()
	out := []store.SnapshotRecord{}
	for rows.Next() {
		rec, err := scanSnapshotRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetSnapshot loads one snapshot record by ID.
func (s *Store) GetSnapshot(ctx context.Context, id string) (store.SnapshotRecord, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,source_session_id,parent_snapshot_id,runtime_snapshot_id,layer_path,size,flattened,created_at,labels_json FROM snapshots WHERE id=$1`, id)
	rec, err := scanSnapshotRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, sql.ErrNoRows
	}
	return rec, err
}

// DeleteSnapshot removes one snapshot record by ID.
func (s *Store) DeleteSnapshot(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM snapshots WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete snapshot: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SnapshotHasChildren reports whether any child session or snapshot references id.
func (s *Store) SnapshotHasChildren(ctx context.Context, id string) (bool, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM snapshots WHERE parent_snapshot_id=$1) + (SELECT COUNT(*) FROM sessions WHERE parent_snapshot_id=$2)`, id, id).Scan(&count); err != nil {
		return false, fmt.Errorf("check snapshot children: %w", err)
	}
	return count > 0, nil
}

func scanSnapshotRow(row rowScanner) (store.SnapshotRecord, error) {
	var r store.SnapshotRecord
	err := row.Scan(&r.ID, &r.SourceSessionID, &r.ParentSnapshotID, &r.RuntimeSnapshotID, &r.LayerPath, &r.Size, &r.Flattened, &r.CreatedAt, &r.LabelsJSON)
	if err != nil {
		return store.SnapshotRecord{}, err
	}
	return r, nil
}
