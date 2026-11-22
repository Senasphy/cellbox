package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

func (s *Store) CreateSnapshot(ctx context.Context, record store.SnapshotRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO snapshots(id,source_session_id,parent_snapshot_id,runtime_snapshot_id,layer_path,size_bytes,flattened,created_at,labels_json) VALUES(?,?,?,?,?,?,?,?,?)`, record.ID, record.SourceSessionID, nullText(record.ParentSnapshotID), record.RuntimeSnapshotID, record.LayerPath, record.Size, record.Flattened, stamp(record.CreatedAt), record.LabelsJSON)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	return nil
}

func (s *Store) ListSnapshots(ctx context.Context, sessionID string) ([]store.SnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source_session_id,COALESCE(parent_snapshot_id,''),runtime_snapshot_id,layer_path,size_bytes,flattened,created_at,labels_json FROM snapshots WHERE source_session_id=? ORDER BY created_at,id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()
	items := []store.SnapshotRecord{}
	for rows.Next() {
		var item store.SnapshotRecord
		var created string
		if err := rows.Scan(&item.ID, &item.SourceSessionID, &item.ParentSnapshotID, &item.RuntimeSnapshotID, &item.LayerPath, &item.Size, &item.Flattened, &created, &item.LabelsJSON); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse snapshot timestamp: %w", err)
		}
		item.CreatedAt = parsed
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshots: %w", err)
	}
	return items, nil
}

func (s *Store) GetSnapshot(ctx context.Context, id string) (store.SnapshotRecord, error) {
	var item store.SnapshotRecord
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,source_session_id,COALESCE(parent_snapshot_id,''),runtime_snapshot_id,layer_path,size_bytes,flattened,created_at,labels_json FROM snapshots WHERE id=?`, id).Scan(&item.ID, &item.SourceSessionID, &item.ParentSnapshotID, &item.RuntimeSnapshotID, &item.LayerPath, &item.Size, &item.Flattened, &created, &item.LabelsJSON)
	if err != nil {
		return item, fmt.Errorf("get snapshot: %w", err)
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return item, fmt.Errorf("parse snapshot timestamp: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteSnapshot(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM snapshots WHERE id=?
		AND NOT EXISTS (SELECT 1 FROM snapshots child WHERE child.parent_snapshot_id=snapshots.id)
		AND NOT EXISTS (SELECT 1 FROM sessions fork WHERE fork.parent_snapshot_id=snapshots.id AND fork.state IN ('creating','ready','stopping'))`, id)
	if err != nil {
		return fmt.Errorf("delete snapshot: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect snapshot deletion: %w", err)
	}
	if count == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM snapshots WHERE id=?`, id).Scan(&exists); err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return fmt.Errorf("snapshot has dependent children")
	}
	return nil
}

func (s *Store) SnapshotHasChildren(ctx context.Context, id string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM snapshots WHERE parent_snapshot_id=?
		UNION ALL
		SELECT 1 FROM sessions WHERE parent_snapshot_id=? AND state IN ('creating','ready','stopping')
	)`, id, id).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("check snapshot children: %w", err)
	}
	return found != 0, nil
}
