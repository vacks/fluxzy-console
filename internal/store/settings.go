package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetSettings writes several keys atomically so a failed update cannot leave
// the hot/cold/sampling policy half-applied.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for key, value := range values {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AddIndexError(ctx context.Context, sessionID, file, message string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// One row per session/file. The watcher retries the same file repeatedly;
	// inserting every failure grew this table without bound.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM index_errors WHERE session_id = ? AND file = ?`, sessionID, file); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO index_errors (session_id, file, message, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, file, message, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteSessionIndexErrors(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM index_errors WHERE session_id = ?`, sessionID)
	return err
}

func (s *Store) CountIndexErrors(ctx context.Context, sessionID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM index_errors WHERE session_id = ?`, sessionID).Scan(&n)
	return n, err
}
