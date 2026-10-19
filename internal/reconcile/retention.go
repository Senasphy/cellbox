package reconcile

import (
	"context"
	"fmt"
	"time"
)

type retentionStore interface {
	PruneAuditEvents(context.Context, time.Time) (int64, error)
	PruneExecOutput(context.Context, time.Time) (int64, error)
}

func SweepRetention(ctx context.Context, storage retentionStore, outputDays, auditDays int, now time.Time) error {
	if storage == nil {
		return nil
	}
	if outputDays > 0 {
		if _, err := storage.PruneExecOutput(ctx, now.Add(-time.Duration(outputDays)*24*time.Hour)); err != nil {
			return fmt.Errorf("prune exec output: %w", err)
		}
	}
	if auditDays > 0 {
		if _, err := storage.PruneAuditEvents(ctx, now.Add(-time.Duration(auditDays)*24*time.Hour)); err != nil {
			return fmt.Errorf("prune audit events: %w", err)
		}
	}
	return nil
}

func RunRetention(ctx context.Context, storage retentionStore, outputDays, auditDays int, report func(error)) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := SweepRetention(ctx, storage, outputDays, auditDays, now); err != nil && report != nil {
				report(err)
			}
		}
	}
}
