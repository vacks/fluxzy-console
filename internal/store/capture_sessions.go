package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type CaptureSession struct {
	ID                string `json:"id"`
	Dir               string `json:"dir"`
	CaptureDate       int64  `json:"captureDate"`
	FluxzyVersion     string `json:"fluxzyVersion"`
	Status            string `json:"status"`
	ExchangeCount     int64  `json:"exchangeCount"`
	TotalBytes        int64  `json:"totalBytes"`
	LastIndexedAt     int64  `json:"lastIndexedAt"`
	MetaJSON          string `json:"-"`
	RulesHash         string `json:"rulesHash"`
	ConfigHash        string `json:"configHash"`
	RotationReason    string `json:"rotationReason"`
	IndexError        string `json:"indexError"`
	IndexingStartedAt int64  `json:"indexingStartedAt"`
	IndexedAt         int64  `json:"indexedAt"`
	DirBytes          int64  `json:"dirBytes"`
	Pinned            bool   `json:"pinned"`
}

const captureSessionColumns = `id, dir, capture_date, fluxzy_version, status, exchange_count, total_bytes,
	last_indexed_at, meta_json, rules_hash, config_hash, rotation_reason, index_error,
	indexing_started_at, indexed_at, dir_bytes, pinned`

func scanCaptureSession(scanner interface{ Scan(...any) error }) (CaptureSession, error) {
	var c CaptureSession
	var pinned int
	err := scanner.Scan(&c.ID, &c.Dir, &c.CaptureDate, &c.FluxzyVersion, &c.Status,
		&c.ExchangeCount, &c.TotalBytes, &c.LastIndexedAt, &c.MetaJSON, &c.RulesHash,
		&c.ConfigHash, &c.RotationReason, &c.IndexError, &c.IndexingStartedAt,
		&c.IndexedAt, &c.DirBytes, &pinned)
	c.Pinned = pinned != 0
	return c, err
}

func (s *Store) UpsertCaptureSession(ctx context.Context, c CaptureSession) error {
	if c.Status == "" {
		c.Status = "indexing"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO capture_sessions
		 (`+captureSessionColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   dir = excluded.dir,
		   capture_date = excluded.capture_date,
		   fluxzy_version = excluded.fluxzy_version,
		   status = excluded.status,
		   meta_json = excluded.meta_json,
		   rules_hash = excluded.rules_hash,
		   config_hash = excluded.config_hash,
		   rotation_reason = excluded.rotation_reason,
		   index_error = excluded.index_error,
		   indexing_started_at = excluded.indexing_started_at,
		   indexed_at = excluded.indexed_at,
		   dir_bytes = excluded.dir_bytes`,
		c.ID, c.Dir, c.CaptureDate, c.FluxzyVersion, c.Status, c.ExchangeCount,
		c.TotalBytes, c.LastIndexedAt, c.MetaJSON, c.RulesHash, c.ConfigHash,
		c.RotationReason, c.IndexError, c.IndexingStartedAt, c.IndexedAt,
		c.DirBytes, boolInt(c.Pinned))
	return err
}

func (s *Store) GetCaptureSession(ctx context.Context, id string) (*CaptureSession, error) {
	c, err := scanCaptureSession(s.db.QueryRowContext(ctx,
		`SELECT `+captureSessionColumns+` FROM capture_sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListCaptureSessions(ctx context.Context, limit int) ([]CaptureSession, error) {
	items, _, err := s.ListCaptureSessionsPage(ctx, limit, 0, "", "", "")
	return items, err
}

// ListCaptureSessionsPage returns one page of sessions plus the total matching
// row count. Filters are applied server-side so pagination totals stay correct
// once the number of sessions exceeds a single page.
func (s *Store) ListCaptureSessionsPage(ctx context.Context, limit, offset int, search, statusFilter, activeID string) ([]CaptureSession, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	where := []string{"1 = 1"}
	args := make([]any, 0, 2)
	if search != "" {
		where = append(where, "instr(lower(id), lower(?)) > 0")
		args = append(args, search)
	}
	switch statusFilter {
	case "live":
		if activeID == "" {
			return []CaptureSession{}, 0, nil
		}
		where = append(where, "id = ?")
		args = append(args, activeID)
	case "archived":
		where = append(where, "id <> ? AND status <> 'expired'")
		args = append(args, activeID)
	case "sampled":
		where = append(where, "status = 'expired'")
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM capture_sessions WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+captureSessionColumns+` FROM capture_sessions WHERE `+clause+
			` ORDER BY capture_date DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]CaptureSession, 0, limit)
	for rows.Next() {
		c, err := scanCaptureSession(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// ListRetentionSessions returns every session that still has data to inspect
// for retention. It deliberately has no LIMIT: the regular session list is
// capped for the UI, but retention must be able to reach old sessions.
func (s *Store) ListRetentionSessions(ctx context.Context) ([]CaptureSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, dir, capture_date, status, indexing_started_at, pinned FROM capture_sessions
		 WHERE status <> 'purged' ORDER BY capture_date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CaptureSession, 0, 64)
	for rows.Next() {
		var c CaptureSession
		var pinned int
		if err := rows.Scan(&c.ID, &c.Dir, &c.CaptureDate, &c.Status, &c.IndexingStartedAt, &pinned); err != nil {
			return nil, err
		}
		c.Pinned = pinned != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CountCaptureSessions(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM capture_sessions`).Scan(&n)
	return n, err
}

func (s *Store) DeleteCaptureSession(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM aggregates WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM index_errors WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM capture_sessions WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetCaptureSessionPinned(ctx context.Context, id string, pinned bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE capture_sessions SET pinned = ? WHERE id = ?`, boolInt(pinned), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateCaptureSessionStats(ctx context.Context, id string, exchangeCount, totalBytes, lastIndexedAt int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE capture_sessions SET exchange_count = ?, total_bytes = ?, last_indexed_at = ?, status = ? WHERE id = ?`,
		exchangeCount, totalBytes, lastIndexedAt, status, id)
	return err
}

func (s *Store) UpdateCaptureSessionIndexStats(ctx context.Context, id string, exchangeCount, totalBytes, lastIndexedAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE capture_sessions SET exchange_count = ?, total_bytes = ?, last_indexed_at = ? WHERE id = ?`,
		exchangeCount, totalBytes, lastIndexedAt, id)
	return err
}

func (s *Store) SetCaptureSessionStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE capture_sessions SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) MarkSessionIndexing(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE capture_sessions SET status = 'indexing', indexing_started_at = ?, index_error = '' WHERE id = ?`,
		now(), id)
	return err
}

func (s *Store) MarkSessionStarting(ctx context.Context, id, dir string, captureDate int64, configHash, rulesHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO capture_sessions
		 (id, dir, capture_date, status, config_hash, rules_hash, indexing_started_at, pinned)
		 VALUES (?, ?, ?, 'starting', ?, ?, ?, 0)
		 ON CONFLICT(id) DO UPDATE SET
		   dir = excluded.dir,
		   capture_date = excluded.capture_date,
		   status = 'starting',
		   config_hash = excluded.config_hash,
		   rules_hash = excluded.rules_hash,
		   indexing_started_at = excluded.indexing_started_at`,
		id, dir, captureDate, configHash, rulesHash, now())
	return err
}

func (s *Store) MarkSessionEnded(ctx context.Context, id, status, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE capture_sessions
		 SET status = ?, rotation_reason = ?, indexed_at = CASE WHEN ? IN ('indexed','expired','purged') THEN ? ELSE indexed_at END
		 WHERE id = ?`,
		status, reason, status, now(), id)
	return err
}

func (s *Store) SetCaptureSessionIndexError(ctx context.Context, id, message string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE capture_sessions SET status = 'error', index_error = ? WHERE id = ?`, message, id)
	return err
}

func (s *Store) SetCaptureSessionRulesHash(ctx context.Context, id, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE capture_sessions SET rules_hash = ? WHERE id = ?`, hash, id)
	return err
}

func (s *Store) UpdateCaptureSessionDirBytes(ctx context.Context, id string, bytes int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE capture_sessions SET dir_bytes = ? WHERE id = ?`, bytes, id)
	return err
}

// DemoteLiveSessions clears the live marker from sessions that are no longer
// the newest capture directory. Without this, every session that was ever
// active keeps status "live" after fluxzy rotates its dump directory.
func (s *Store) DemoteLiveSessions(ctx context.Context, activeID string) error {
	// A live marker from a previous process does not prove its FullScan
	// completed. Mark it stopped so loadExisting queues it for re-indexing.
	_, err := s.db.ExecContext(ctx,
		`UPDATE capture_sessions SET status = 'stopped' WHERE status = 'live' AND id <> ?`,
		activeID)
	return err
}
