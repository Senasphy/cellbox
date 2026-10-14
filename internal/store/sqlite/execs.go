package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/senasphy/cellbox/internal/store"
)

// CreateExec persists an exec before output is streamed.
func (s *Store) CreateExec(ctx context.Context, v store.ExecRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO execs(id,session_id,argv_json,shell,cwd,env_json,tty,state,started_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.ID, v.SessionID, v.ArgvJSON, v.Shell, v.Cwd, v.EnvJSON, v.TTY, v.State, stamp(v.StartedAt))
	if err != nil {
		return fmt.Errorf("insert exec: %w", err)
	}
	return nil
}

// UpdateExec persists an exec's state and result.
func (s *Store) UpdateExec(ctx context.Context, v store.ExecRecord) error {
	var ended any
	if v.EndedAt != nil {
		ended = stamp(*v.EndedAt)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE execs SET state=?,exit_code=?,signal=?,ended_at=?,output_bytes=?,truncated=? WHERE id=?`, v.State, v.ExitCode, v.Signal, ended, v.OutputBytes, v.Truncated, v.ID)
	if err != nil {
		return fmt.Errorf("update exec: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check exec update: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetExec returns a persisted command result.
func (s *Store) GetExec(ctx context.Context, id string) (store.ExecRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,session_id,argv_json,shell,cwd,env_json,tty,state,exit_code,signal,started_at,ended_at,output_bytes,truncated FROM execs WHERE id=?`, id)
	v, err := scanExec(row)
	if err != nil {
		return v, fmt.Errorf("get exec: %w", err)
	}
	return v, nil
}

// ListExecs returns executions for one session in start order.
func (s *Store) ListExecs(ctx context.Context, sessionID string) ([]store.ExecRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,session_id,argv_json,shell,cwd,env_json,tty,state,exit_code,signal,started_at,ended_at,output_bytes,truncated FROM execs WHERE session_id=? ORDER BY started_at,id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list execs: %w", err)
	}
	defer rows.Close()
	items := []store.ExecRecord{}
	for rows.Next() {
		v, e := scanExec(rows)
		if e != nil {
			return nil, fmt.Errorf("scan exec: %w", e)
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execs: %w", err)
	}
	return items, nil
}

// AppendExecOutput atomically stores a chunk at the next byte offset before delivery.
func (s *Store) AppendExecOutput(ctx context.Context, id, stream string, data []byte, ts time.Time) (store.ExecOutputRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("begin output append: %w", err)
	}
	defer tx.Rollback()
	var offset int64
	if err = tx.QueryRowContext(ctx, `SELECT output_bytes FROM execs WHERE id=?`, id).Scan(&offset); err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("read exec output offset: %w", err)
	}
	record := store.ExecOutputRecord{ExecID: id, Offset: offset, Stream: stream, Data: append([]byte(nil), data...), Timestamp: ts.UTC()}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_output(exec_id,byte_offset,stream,data,timestamp) VALUES(?,?,?,?,?)`, id, offset, stream, data, stamp(ts)); err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("append exec output: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE execs SET output_bytes=output_bytes+? WHERE id=?`, len(data), id); err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("advance exec output offset: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.ExecOutputRecord{}, fmt.Errorf("commit exec output: %w", err)
	}
	return record, nil
}

// ListExecOutput reads chunks whose byte ranges extend beyond the resume offset.
func (s *Store) ListExecOutput(ctx context.Context, id string, after int64) ([]store.ExecOutputRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT exec_id,byte_offset,stream,data,timestamp FROM exec_output WHERE exec_id=? AND byte_offset+length(data)>? ORDER BY byte_offset`, id, after)
	if err != nil {
		return nil, fmt.Errorf("list exec output: %w", err)
	}
	defer rows.Close()
	items := []store.ExecOutputRecord{}
	for rows.Next() {
		var v store.ExecOutputRecord
		var ts string
		if err = rows.Scan(&v.ExecID, &v.Offset, &v.Stream, &v.Data, &ts); err != nil {
			return nil, fmt.Errorf("scan exec output: %w", err)
		}
		v.Timestamp, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parse output timestamp: %w", err)
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exec output: %w", err)
	}
	return items, nil
}

func scanExec(row scanner) (store.ExecRecord, error) {
	var v store.ExecRecord
	var tty, truncated int
	var exitCode, signal sql.NullInt64
	var started string
	var ended sql.NullString
	err := row.Scan(&v.ID, &v.SessionID, &v.ArgvJSON, &v.Shell, &v.Cwd, &v.EnvJSON, &tty, &v.State, &exitCode, &signal, &started, &ended, &v.OutputBytes, &truncated)
	if err != nil {
		return v, err
	}
	v.TTY = tty != 0
	v.Truncated = truncated != 0
	if exitCode.Valid {
		n := int(exitCode.Int64)
		v.ExitCode = &n
	}
	if signal.Valid {
		n := int(signal.Int64)
		v.Signal = &n
	}
	if v.StartedAt, err = time.Parse(time.RFC3339Nano, started); err != nil {
		return v, fmt.Errorf("parse exec start time: %w", err)
	}
	if ended.Valid {
		t, e := time.Parse(time.RFC3339Nano, ended.String)
		if e != nil {
			return v, fmt.Errorf("parse exec end time: %w", e)
		}
		v.EndedAt = &t
	}
	return v, nil
}
