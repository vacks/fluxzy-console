package store

import (
	"context"
	"database/sql"
	"errors"
)

type RuntimeStatus struct {
	State               string
	PID                 int
	ActiveSessionID     string
	BinaryVersion       string
	BinaryRelease       string
	BinaryPath          string
	StartedAt           int64
	LastExitCode        *int
	LastError           string
	ConsecutiveFailures int
	UpdatedAt           int64
}

func (s *Store) SaveRuntimeStatus(ctx context.Context, st RuntimeStatus) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fluxzy_runtime_status
		 (id, state, pid, active_session_id, binary_version, binary_release, binary_path,
		  started_at, last_exit_code, last_error, consecutive_failures, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   state = excluded.state,
		   pid = excluded.pid,
		   active_session_id = excluded.active_session_id,
		   binary_version = excluded.binary_version,
		   binary_release = excluded.binary_release,
		   binary_path = excluded.binary_path,
		   started_at = excluded.started_at,
		   last_exit_code = excluded.last_exit_code,
		   last_error = excluded.last_error,
		   consecutive_failures = excluded.consecutive_failures,
		   updated_at = excluded.updated_at`,
		st.State, st.PID, st.ActiveSessionID, st.BinaryVersion, st.BinaryRelease,
		st.BinaryPath, st.StartedAt, st.LastExitCode, st.LastError,
		st.ConsecutiveFailures, now())
	return err
}

func (s *Store) GetRuntimeStatus(ctx context.Context) (*RuntimeStatus, error) {
	var st RuntimeStatus
	err := s.db.QueryRowContext(ctx,
		`SELECT state, pid, active_session_id, binary_version, binary_release, binary_path,
		        started_at, last_exit_code, last_error, consecutive_failures, updated_at
		 FROM fluxzy_runtime_status WHERE id = 1`).
		Scan(&st.State, &st.PID, &st.ActiveSessionID, &st.BinaryVersion, &st.BinaryRelease,
			&st.BinaryPath, &st.StartedAt, &st.LastExitCode, &st.LastError,
			&st.ConsecutiveFailures, &st.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

type SupervisorEvent struct {
	ID        int64
	Kind      string
	SessionID string
	PID       int
	ExitCode  *int
	Message   string
	CreatedAt int64
}

func (s *Store) AppendSupervisorEvent(ctx context.Context, ev SupervisorEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fluxzy_supervisor_events (kind, session_id, pid, exit_code, message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ev.Kind, ev.SessionID, ev.PID, ev.ExitCode, ev.Message, now())
	return err
}

func (s *Store) PurgeSupervisorEvents(ctx context.Context, before int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM fluxzy_supervisor_events WHERE created_at < ?`, before)
	return err
}

func (s *Store) ListSupervisorEvents(ctx context.Context, limit int) ([]SupervisorEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, session_id, pid, exit_code, message, created_at
		 FROM fluxzy_supervisor_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SupervisorEvent, 0, limit)
	for rows.Next() {
		var ev SupervisorEvent
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.SessionID, &ev.PID, &ev.ExitCode, &ev.Message, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
