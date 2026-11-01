package store

import (
	"context"
	"database/sql"
	"errors"
)

type ConfigVersion struct {
	ID               int64  `json:"id"`
	YAML             string `json:"yaml,omitempty"`
	YAMLHash         string `json:"yamlHash"`
	AuthorID         *int64 `json:"authorId,omitempty"`
	AuthorName       string `json:"authorName"`
	Note             string `json:"note"`
	ValidationStatus string `json:"validationStatus"`
	ValidationError  string `json:"validationError"`
	Status           string `json:"status"`
	ActiveSessionID  string `json:"activeSessionId"`
	AppliedAt        int64  `json:"appliedAt"`
	AppliedSessionID string `json:"appliedSessionId"`
	AppliedError     string `json:"appliedError"`
	CreatedAt        int64  `json:"createdAt"`
}

func (s *Store) CreateConfigVersion(ctx context.Context, v ConfigVersion) (*ConfigVersion, error) {
	if v.ValidationStatus == "" {
		v.ValidationStatus = "unknown"
	}
	if v.Status == "" {
		v.Status = "pending"
	}
	ts := now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO config_versions
		 (yaml, yaml_hash, author_id, author_name, note, validation_status, validation_error, status, active_session_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.YAML, v.YAMLHash, v.AuthorID, v.AuthorName, v.Note, v.ValidationStatus,
		v.ValidationError, v.Status, v.ActiveSessionID, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	v.ID = id
	v.CreatedAt = ts
	return &v, nil
}

func (s *Store) GetConfigVersion(ctx context.Context, id int64) (*ConfigVersion, error) {
	v := &ConfigVersion{}
	var author sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, yaml, yaml_hash, author_id, author_name, note, validation_status, validation_error, status, active_session_id, applied_at, applied_session_id, applied_error, created_at
		 FROM config_versions WHERE id = ?`, id).
		Scan(&v.ID, &v.YAML, &v.YAMLHash, &author, &v.AuthorName, &v.Note,
			&v.ValidationStatus, &v.ValidationError, &v.Status, &v.ActiveSessionID,
			&v.AppliedAt, &v.AppliedSessionID, &v.AppliedError, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if author.Valid {
		v.AuthorID = &author.Int64
	}
	return v, nil
}

func (s *Store) ListConfigVersions(ctx context.Context, limit int) ([]ConfigVersion, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, yaml_hash, author_id, author_name, note, validation_status, validation_error, status, active_session_id, applied_at, applied_session_id, applied_error, created_at
		 FROM config_versions ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ConfigVersion, 0, 16)
	for rows.Next() {
		var v ConfigVersion
		var author sql.NullInt64
		if err := rows.Scan(&v.ID, &v.YAMLHash, &author, &v.AuthorName, &v.Note,
			&v.ValidationStatus, &v.ValidationError, &v.Status, &v.ActiveSessionID,
			&v.AppliedAt, &v.AppliedSessionID, &v.AppliedError, &v.CreatedAt); err != nil {
			return nil, err
		}
		if author.Valid {
			v.AuthorID = &author.Int64
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) LatestConfigVersion(ctx context.Context) (*ConfigVersion, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM config_versions ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetConfigVersion(ctx, id)
}

func (s *Store) FindConfigVersionByHash(ctx context.Context, hash string) (*ConfigVersion, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM config_versions WHERE yaml_hash = ? ORDER BY id DESC LIMIT 1`, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetConfigVersion(ctx, id)
}

func (s *Store) SetConfigVersionStatus(ctx context.Context, id int64, status, activeSessionID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE config_versions SET status = ?, active_session_id = ? WHERE id = ?`, status, activeSessionID, id)
	return err
}

func (s *Store) MarkConfigVersionApplied(ctx context.Context, id int64, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE config_versions
		 SET status = 'applied', active_session_id = ?, applied_at = ?, applied_session_id = ?, applied_error = ''
		 WHERE id = ?`, sessionID, now(), sessionID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE config_versions SET status = 'superseded'
		 WHERE status = 'applied' AND id <> ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkConfigVersionFailed(ctx context.Context, id int64, message string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE config_versions SET status = 'failed', applied_error = ? WHERE id = ?`, message, id)
	return err
}
