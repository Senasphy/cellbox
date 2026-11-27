package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/senasphy/cellbox/internal/store"
)

// CreateExec persists an initial exec record.
func (s *Store) CreateExec(ctx context.Context, v store.ExecRecord) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO execs(id,session_id,argv_json,shell,cwd,env_json,tty,state,exit_code,signal,started_at,ended_at,output_bytes,truncated,peak_memory_bytes)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		v.ID, v.SessionID, jsonOrDefault(v.ArgvJSON, `[]`), v.Shell, v.Cwd, jsonOrDefault(v.EnvJSON, `{}`),
		v.TTY, v.State, v.ExitCode, v.Signal, v.StartedAt.UTC(), v.EndedAt, v.OutputBytes, v.Truncated, v.PeakMemoryBytes,
	)
	if err != nil {
		return fmt.Errorf("insert exec: %w", err)
	}
	return nil
}

// UpdateExec persists exec completion or status updates.
func (s *Store) UpdateExec(ctx context.Context, v store.ExecRecord) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE execs SET state=$1,exit_code=$2,signal=$3,ended_at=$4,output_bytes=$5,truncated=$6,peak_memory_bytes=$7 WHERE id=$8`,
		v.State, v.ExitCode, v.Signal, v.EndedAt, v.OutputBytes, v.Truncated, v.PeakMemoryBytes, v.ID,
	)
	if err != nil {
		return fmt.Errorf("update exec: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetExec loads one exec record by ID.
func (s *Store) GetExec(ctx context.Context, id string) (store.ExecRecord, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,session_id,argv_json,shell,cwd,env_json,tty,state,exit_code,signal,started_at,ended_at,output_bytes,truncated,peak_memory_bytes FROM execs WHERE id=$1`, id)
	rec, err := scanExecRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, sql.ErrNoRows
	}
	return rec, err
}

// ListExecs returns all execs for one session ordered oldest-first.
func (s *Store) ListExecs(ctx context.Context, sessionID string) ([]store.ExecRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,session_id,argv_json,shell,cwd,env_json,tty,state,exit_code,signal,started_at,ended_at,output_bytes,truncated,peak_memory_bytes FROM execs WHERE session_id=$1 ORDER BY started_at ASC, id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list execs: %w", err)
	}
	defer rows.Close()
	out := []store.ExecRecord{}
	for rows.Next() {
		rec, err := scanExecRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// PageExecs returns one keyset-paginated slice of execs for a session.
func (s *Store) PageExecs(ctx context.Context, sessionID string, limit int, cursor *store.ListCursor) ([]store.ExecRecord, bool, error) {
	limit = normalizeLimit(limit)
	query := `SELECT id,session_id,argv_json,shell,cwd,env_json,tty,state,exit_code,signal,started_at,ended_at,output_bytes,truncated,peak_memory_bytes FROM execs WHERE session_id=$1`
	args := []any{sessionID}
	if cursor != nil {
		query += ` AND (started_at > $2 OR (started_at = $3 AND id > $4)) ORDER BY started_at ASC, id ASC LIMIT $5`
		args = append(args, cursor.Timestamp.UTC(), cursor.Timestamp.UTC(), cursor.ID, limit+1)
	} else {
		query += ` ORDER BY started_at ASC, id ASC LIMIT $2`
		args = append(args, limit+1)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("page execs: %w", err)
	}
	defer rows.Close()
	out := make([]store.ExecRecord, 0, limit+1)
	for rows.Next() {
		rec, err := scanExecRow(rows)
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

// AppendExecOutput appends one output chunk at the next monotonic byte offset.
func (s *Store) AppendExecOutput(ctx context.Context, id, stream string, data []byte, ts time.Time) (store.ExecOutputRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("begin exec output append: %w", err)
	}
	defer tx.Rollback(ctx)
	var offset int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(offset_bytes), 0) FROM exec_output WHERE exec_id=$1`, id).Scan(&offset); err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("read exec output offset: %w", err)
	}
	offset += int64(len(data))
	if _, err := tx.Exec(ctx, `INSERT INTO exec_output(exec_id,offset_bytes,stream,data,timestamp) VALUES($1,$2,$3,$4,$5)`, id, offset, stream, data, ts.UTC()); err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("insert exec output: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("commit exec output: %w", err)
	}
	return store.ExecOutputRecord{ExecID: id, Offset: offset, Stream: stream, Data: append([]byte(nil), data...), Timestamp: ts.UTC()}, nil
}

// ListExecOutput returns persisted output chunks after the given byte offset.
func (s *Store) ListExecOutput(ctx context.Context, id string, after int64) ([]store.ExecOutputRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT exec_id,offset_bytes,stream,data,timestamp FROM exec_output WHERE exec_id=$1 AND offset_bytes>$2 ORDER BY offset_bytes ASC`, id, after)
	if err != nil {
		return nil, fmt.Errorf("list exec output: %w", err)
	}
	defer rows.Close()
	out := []store.ExecOutputRecord{}
	for rows.Next() {
		var rec store.ExecOutputRecord
		if err := rows.Scan(&rec.ExecID, &rec.Offset, &rec.Stream, &rec.Data, &rec.Timestamp); err != nil {
			return nil, fmt.Errorf("scan exec output: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func scanExecRow(row rowScanner) (store.ExecRecord, error) {
	var v store.ExecRecord
	err := row.Scan(
		&v.ID, &v.SessionID, &v.ArgvJSON, &v.Shell, &v.Cwd, &v.EnvJSON,
		&v.TTY, &v.State, &v.ExitCode, &v.Signal, &v.StartedAt, &v.EndedAt,
		&v.OutputBytes, &v.Truncated, &v.PeakMemoryBytes,
	)
	if err != nil {
		return store.ExecRecord{}, err
	}
	return v, nil
}
