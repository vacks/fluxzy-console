package store

import (
	"context"
	"database/sql"
	"strings"
)

type AuditEntry struct {
	ID        int64  `json:"id"`
	UserID    *int64 `json:"userId,omitempty"`
	Username  string `json:"username"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	IP        string `json:"ip"`
	UserAgent string `json:"userAgent"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) AddAudit(ctx context.Context, e AuditEntry) error {
	e.Username = truncate(e.Username, 64)
	e.Action = truncate(e.Action, 64)
	e.Target = truncate(e.Target, 256)
	e.Detail = truncate(e.Detail, 1024)
	e.IP = truncate(e.IP, 64)
	e.UserAgent = truncate(e.UserAgent, 256)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, username, action, target, detail, ip, user_agent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.Username, e.Action, e.Target, e.Detail, e.IP, e.UserAgent, now())
	return err
}

func truncate(v string, max int) string {
	r := []rune(v)
	if len(r) <= max {
		return v
	}
	return string(r[:max])
}

func (s *Store) ListAudit(ctx context.Context, limit int, beforeID int64) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if beforeID > 0 {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, user_id, username, action, target, detail, ip, user_agent, created_at
			 FROM audit_log WHERE id < ? ORDER BY id DESC LIMIT ?`, beforeID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, user_id, username, action, target, detail, ip, user_agent, created_at
			 FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AuditEntry, 0, 32)
	for rows.Next() {
		var e AuditEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &uid, &e.Username, &e.Action, &e.Target, &e.Detail, &e.IP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, err
		}
		if uid.Valid {
			e.UserID = &uid.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListAuditPage returns one page of audit entries plus the total matching
// count. Search and action filters run in SQL so pagination totals stay
// correct as the log grows.
func (s *Store) ListAuditPage(ctx context.Context, limit, offset int, search, action string) ([]AuditEntry, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	search = strings.TrimSpace(search)
	if len(search) > 128 {
		search = search[:128]
	}
	where := []string{"1 = 1"}
	args := make([]any, 0, 2)
	if search != "" {
		where = append(where,
			"instr(lower(username || ' ' || action || ' ' || target || ' ' || ip), lower(?)) > 0")
		args = append(args, search)
	}
	if action != "" {
		where = append(where, "action = ?")
		args = append(args, action)
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, username, action, target, detail, ip, user_agent, created_at
		 FROM audit_log WHERE `+clause+` ORDER BY id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var e AuditEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &uid, &e.Username, &e.Action, &e.Target, &e.Detail,
			&e.IP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		if uid.Valid {
			e.UserID = &uid.Int64
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *Store) PurgeAudit(ctx context.Context, cutoff int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE created_at < ?`, cutoff)
	return err
}

func (s *Store) ListAuditActions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT action FROM audit_log ORDER BY action ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 16)
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
