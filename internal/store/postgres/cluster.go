package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ClusterWorkerRecord persists one worker's heartbeat and capacity state.
type ClusterWorkerRecord struct {
	ID              string
	AdvertiseURL    string
	GRPCAddr        string
	State           string
	TotalCPUMilli   int64
	TotalMemoryMB   int64
	TotalPIDs       int64
	UsedCPUMilli    int64
	UsedMemoryMB    int64
	UsedPIDs        int64
	ActiveSessions  int
	LastHeartbeatAt time.Time
}

// UpsertWorker registers or refreshes a cluster worker heartbeat.
func (s *Store) UpsertWorker(ctx context.Context, w ClusterWorkerRecord) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO cluster_workers(id,advertise_url,grpc_addr,state,total_cpu_milli,total_memory_mb,total_pids,used_cpu_milli,used_memory_mb,used_pids,active_sessions,last_heartbeat_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (id) DO UPDATE SET
		   advertise_url=EXCLUDED.advertise_url,
		   grpc_addr=EXCLUDED.grpc_addr,
		   state=CASE WHEN cluster_workers.state='draining' AND EXCLUDED.state='ready' THEN 'draining' ELSE EXCLUDED.state END,
		   total_cpu_milli=EXCLUDED.total_cpu_milli,
		   total_memory_mb=EXCLUDED.total_memory_mb,
		   total_pids=EXCLUDED.total_pids,
		   used_cpu_milli=EXCLUDED.used_cpu_milli,
		   used_memory_mb=EXCLUDED.used_memory_mb,
		   used_pids=EXCLUDED.used_pids,
		   active_sessions=EXCLUDED.active_sessions,
		   last_heartbeat_at=EXCLUDED.last_heartbeat_at`,
		w.ID, w.AdvertiseURL, w.GRPCAddr, w.State,
		w.TotalCPUMilli, w.TotalMemoryMB, w.TotalPIDs,
		w.UsedCPUMilli, w.UsedMemoryMB, w.UsedPIDs,
		w.ActiveSessions, w.LastHeartbeatAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert cluster worker: %w", err)
	}
	return nil
}

// ListWorkers returns all known cluster workers ordered by ID.
func (s *Store) ListWorkers(ctx context.Context) ([]ClusterWorkerRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,advertise_url,grpc_addr,state,total_cpu_milli,total_memory_mb,total_pids,used_cpu_milli,used_memory_mb,used_pids,active_sessions,last_heartbeat_at FROM cluster_workers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list cluster workers: %w", err)
	}
	defer rows.Close()
	out := []ClusterWorkerRecord{}
	for rows.Next() {
		var w ClusterWorkerRecord
		if err := rows.Scan(&w.ID, &w.AdvertiseURL, &w.GRPCAddr, &w.State, &w.TotalCPUMilli, &w.TotalMemoryMB, &w.TotalPIDs, &w.UsedCPUMilli, &w.UsedMemoryMB, &w.UsedPIDs, &w.ActiveSessions, &w.LastHeartbeatAt); err != nil {
			return nil, fmt.Errorf("scan cluster worker: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkWorkerDraining transitions a worker to draining so no new sessions are placed on it.
func (s *Store) MarkWorkerDraining(ctx context.Context, workerID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE cluster_workers SET state='draining' WHERE id=$1 AND state != 'lost'`, workerID)
	if err != nil {
		return fmt.Errorf("mark worker draining: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkLostWorkers marks workers with stale heartbeats as lost and marks their active sessions lost.
func (s *Store) MarkLostWorkers(ctx context.Context, cutoff time.Time) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin mark lost workers: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `UPDATE cluster_workers SET state='lost' WHERE state IN ('ready','draining') AND last_heartbeat_at < $1 RETURNING id`, cutoff.UTC())
	if err != nil {
		return nil, fmt.Errorf("mark stale workers lost: %w", err)
	}
	var lost []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		lost = append(lost, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, workerID := range lost {
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET state='lost', failure_reason='worker_lost', last_activity_at=$1
			 WHERE id IN (SELECT session_id FROM session_placements WHERE worker_id=$2)
			   AND state IN ('creating','ready','stopping')`,
			time.Now().UTC(), workerID,
		); err != nil {
			return nil, fmt.Errorf("mark worker %s sessions lost: %w", workerID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit lost workers: %w", err)
	}
	return lost, nil
}

// AssignSessionWorker records the owning worker for a session.
func (s *Store) AssignSessionWorker(ctx context.Context, sessionID, workerID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO session_placements(session_id,worker_id,assigned_at) VALUES($1,$2,$3)
		 ON CONFLICT (session_id) DO UPDATE SET worker_id=EXCLUDED.worker_id, assigned_at=EXCLUDED.assigned_at`,
		sessionID, workerID, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("assign session worker: %w", err)
	}
	return nil
}

// GetSessionWorker returns the owning worker ID for a session.
func (s *Store) GetSessionWorker(ctx context.Context, sessionID string) (string, error) {
	var workerID string
	err := s.pool.QueryRow(ctx, `SELECT worker_id FROM session_placements WHERE session_id=$1`, sessionID).Scan(&workerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("get session worker: %w", err)
	}
	return workerID, nil
}
