package sqlite

import (
	"context"
	"fmt"
	"time"
)

func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE timestamp < ?`, stamp(before.UTC()))
	if err != nil {
		return 0, fmt.Errorf("prune audit events: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned audit events: %w", err)
	}
	return count, nil
}

func (s *Store) PruneExecOutput(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM exec_output WHERE timestamp < ?`, stamp(before.UTC()))
	if err != nil {
		return 0, fmt.Errorf("prune exec output: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned exec output: %w", err)
	}
	return count, nil
}
