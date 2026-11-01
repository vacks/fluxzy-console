package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// ExchangeRow is the compact metadata kept in the per-session index. Headers
// and bodies stay in fluxzy's .mpack/.data files; the detail endpoint reads
// them on demand.
type ExchangeRow struct {
	ID           int64    `json:"id"`
	ConnectionID int      `json:"connectionId"`
	Method       string   `json:"method"`
	Scheme       string   `json:"scheme"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Path         string   `json:"path"`
	URL          string   `json:"url"`
	Status       int      `json:"status"`
	ContentType  string   `json:"contentType"`
	ReqSize      int64    `json:"reqSize"`
	RespSize     int64    `json:"respSize"`
	DurationMs   int64    `json:"durationMs"`
	StartedAt    int64    `json:"startedAt"`
	Pending      bool     `json:"pending"`
	IsWebSocket  bool     `json:"isWebSocket"`
	HasError     bool     `json:"hasError"`
	Tags         []string `json:"tags"`
	Agent        string   `json:"agent"`
	HTTPVersion  string   `json:"httpVersion"`
	EgressIP     string   `json:"egressIp"`
	Comment      string   `json:"comment"`
	Secure       bool     `json:"secure"`

	FileMTime int64 `json:"-"`
	FileSize  int64 `json:"-"`
}

type ExchangeFilter struct {
	Limit       int
	BeforeID    int64
	AfterID     int64
	Query       string
	Method      string
	Status      int
	StatusCls   string // 2xx, 3xx, 4xx, 5xx
	Host        string
	PendingOnly bool
	ErrorsOnly  bool
}

type IndexStore struct {
	db   *sql.DB
	path string
}

// indexSchemaVersion is stored in SQLite's user_version. Migrations are only
// executed when an index was created by an older console; without this check
// every archived-session request re-ran the DDL and the totals initializer.
const indexSchemaVersion = 2

func OpenIndex(path string) (*IndexStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create index dir: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	x := &IndexStore{db: db, path: path}
	if err := x.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return x, nil
}

// OpenIndexReadOnly opens an existing index without creating or migrating it.
// Read paths must not turn a missing index into an empty database.
func OpenIndexReadOnly(path string) (*IndexStore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		_ = db.Close()
		return nil, err
	}
	if version > indexSchemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf("index schema version %d is newer than supported version %d", version, indexSchemaVersion)
	}
	return &IndexStore{db: db, path: path}, nil
}

func (x *IndexStore) Close() error { return x.db.Close() }

func (x *IndexStore) migrate() error {
	var version int
	if err := x.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("index schema version: %w", err)
	}
	if version == indexSchemaVersion {
		return nil
	}
	if version > indexSchemaVersion {
		return fmt.Errorf("index schema version %d is newer than supported version %d", version, indexSchemaVersion)
	}

	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS exchanges (
			id INTEGER PRIMARY KEY,
			connection_id INTEGER NOT NULL DEFAULT 0,
			method TEXT NOT NULL DEFAULT '',
			scheme TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			port INTEGER NOT NULL DEFAULT 0,
			path TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 0,
			content_type TEXT NOT NULL DEFAULT '',
			req_size INTEGER NOT NULL DEFAULT 0,
			resp_size INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			started_at INTEGER NOT NULL DEFAULT 0,
			pending INTEGER NOT NULL DEFAULT 0,
			is_websocket INTEGER NOT NULL DEFAULT 0,
			has_error INTEGER NOT NULL DEFAULT 0,
			tags TEXT NOT NULL DEFAULT '[]',
			agent TEXT NOT NULL DEFAULT '',
			http_version TEXT NOT NULL DEFAULT '',
			egress_ip TEXT NOT NULL DEFAULT '',
			comment TEXT NOT NULL DEFAULT '',
			secure INTEGER NOT NULL DEFAULT 0,
			file_mtime INTEGER NOT NULL DEFAULT 0,
			file_size INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_exchanges_started ON exchanges(started_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_exchanges_host ON exchanges(host)`,
		`CREATE INDEX IF NOT EXISTS idx_exchanges_status ON exchanges(status)`,
		`CREATE INDEX IF NOT EXISTS idx_exchanges_method ON exchanges(method)`,
		`CREATE INDEX IF NOT EXISTS idx_exchanges_method_nocase ON exchanges(method COLLATE NOCASE)`,
		`CREATE INDEX IF NOT EXISTS idx_exchanges_pending ON exchanges(pending)`,
		`CREATE TABLE IF NOT EXISTS totals (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			count INTEGER NOT NULL DEFAULT 0,
			req_bytes INTEGER NOT NULL DEFAULT 0,
			resp_bytes INTEGER NOT NULL DEFAULT 0,
			s2 INTEGER NOT NULL DEFAULT 0,
			s3 INTEGER NOT NULL DEFAULT 0,
			s4 INTEGER NOT NULL DEFAULT 0,
			s5 INTEGER NOT NULL DEFAULT 0,
			errors INTEGER NOT NULL DEFAULT 0,
			pending INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT OR IGNORE INTO totals (id, count, req_bytes, resp_bytes, s2, s3, s4, s5, errors, pending)
		 SELECT 1, COUNT(*), COALESCE(SUM(req_size), 0), COALESCE(SUM(resp_size), 0),
		        COALESCE(SUM(CASE WHEN status >= 200 AND status < 300 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status >= 300 AND status < 400 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status >= 400 AND status < 500 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN has_error <> 0 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN pending <> 0 THEN 1 ELSE 0 END), 0)
		 FROM exchanges`,
		`CREATE TABLE IF NOT EXISTS host_counts (
			host TEXT PRIMARY KEY,
			count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_host_counts_count ON host_counts(count DESC, host)`,
		`INSERT OR IGNORE INTO host_counts (host, count)
		 SELECT host, COUNT(*) FROM exchanges GROUP BY host`,
		`CREATE TRIGGER IF NOT EXISTS trg_host_counts_ai AFTER INSERT ON exchanges BEGIN
		 INSERT INTO host_counts (host, count) VALUES (NEW.host, 1)
		 ON CONFLICT(host) DO UPDATE SET count = count + 1;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_host_counts_au AFTER UPDATE OF host ON exchanges
		 WHEN OLD.host <> NEW.host BEGIN
		 UPDATE host_counts SET count = count - 1 WHERE host = OLD.host;
		 DELETE FROM host_counts WHERE host = OLD.host AND count <= 0;
		 INSERT INTO host_counts (host, count) VALUES (NEW.host, 1)
		 ON CONFLICT(host) DO UPDATE SET count = count + 1;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_host_counts_ad AFTER DELETE ON exchanges BEGIN
		 UPDATE host_counts SET count = count - 1 WHERE host = OLD.host;
		 DELETE FROM host_counts WHERE host = OLD.host AND count <= 0;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_exchanges_ai AFTER INSERT ON exchanges BEGIN
		 INSERT INTO totals (id, count, req_bytes, resp_bytes, s2, s3, s4, s5, errors, pending)
		 VALUES (1, 1, NEW.req_size, NEW.resp_size,
		   CASE WHEN NEW.status >= 200 AND NEW.status < 300 THEN 1 ELSE 0 END,
		   CASE WHEN NEW.status >= 300 AND NEW.status < 400 THEN 1 ELSE 0 END,
		   CASE WHEN NEW.status >= 400 AND NEW.status < 500 THEN 1 ELSE 0 END,
		   CASE WHEN NEW.status >= 500 THEN 1 ELSE 0 END,
		   CASE WHEN NEW.has_error <> 0 THEN 1 ELSE 0 END,
		   CASE WHEN NEW.pending <> 0 THEN 1 ELSE 0 END)
		 ON CONFLICT(id) DO UPDATE SET
		   count = count + 1,
		   req_bytes = req_bytes + excluded.req_bytes,
		   resp_bytes = resp_bytes + excluded.resp_bytes,
		   s2 = s2 + excluded.s2,
		   s3 = s3 + excluded.s3,
		   s4 = s4 + excluded.s4,
		   s5 = s5 + excluded.s5,
		   errors = errors + excluded.errors,
		   pending = pending + excluded.pending;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_exchanges_au AFTER UPDATE ON exchanges BEGIN
		 UPDATE totals SET
		   req_bytes = req_bytes - OLD.req_size + NEW.req_size,
		   resp_bytes = resp_bytes - OLD.resp_size + NEW.resp_size,
		   s2 = s2 - CASE WHEN OLD.status >= 200 AND OLD.status < 300 THEN 1 ELSE 0 END
		           + CASE WHEN NEW.status >= 200 AND NEW.status < 300 THEN 1 ELSE 0 END,
		   s3 = s3 - CASE WHEN OLD.status >= 300 AND OLD.status < 400 THEN 1 ELSE 0 END
		           + CASE WHEN NEW.status >= 300 AND NEW.status < 400 THEN 1 ELSE 0 END,
		   s4 = s4 - CASE WHEN OLD.status >= 400 AND OLD.status < 500 THEN 1 ELSE 0 END
		           + CASE WHEN NEW.status >= 400 AND NEW.status < 500 THEN 1 ELSE 0 END,
		   s5 = s5 - CASE WHEN OLD.status >= 500 THEN 1 ELSE 0 END
		           + CASE WHEN NEW.status >= 500 THEN 1 ELSE 0 END,
		   errors = errors - CASE WHEN OLD.has_error <> 0 THEN 1 ELSE 0 END
		                 + CASE WHEN NEW.has_error <> 0 THEN 1 ELSE 0 END,
		   pending = pending - CASE WHEN OLD.pending <> 0 THEN 1 ELSE 0 END
		                   + CASE WHEN NEW.pending <> 0 THEN 1 ELSE 0 END
		 WHERE id = 1;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_exchanges_ad AFTER DELETE ON exchanges BEGIN
		 UPDATE totals SET
		   count = count - 1,
		   req_bytes = req_bytes - OLD.req_size,
		   resp_bytes = resp_bytes - OLD.resp_size,
		   s2 = s2 - CASE WHEN OLD.status >= 200 AND OLD.status < 300 THEN 1 ELSE 0 END,
		   s3 = s3 - CASE WHEN OLD.status >= 300 AND OLD.status < 400 THEN 1 ELSE 0 END,
		   s4 = s4 - CASE WHEN OLD.status >= 400 AND OLD.status < 500 THEN 1 ELSE 0 END,
		   s5 = s5 - CASE WHEN OLD.status >= 500 THEN 1 ELSE 0 END,
		   errors = errors - CASE WHEN OLD.has_error <> 0 THEN 1 ELSE 0 END,
		   pending = pending - CASE WHEN OLD.pending <> 0 THEN 1 ELSE 0 END
		 WHERE id = 1;
		END`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("index migrate: %w", err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, indexSchemaVersion)); err != nil {
		return fmt.Errorf("index schema version: %w", err)
	}
	return tx.Commit()
}

func (x *IndexStore) UpsertBatch(ctx context.Context, rows []ExchangeRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := x.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO exchanges
		(id, connection_id, method, scheme, host, port, path, url, status, content_type,
		 req_size, resp_size, duration_ms, started_at, pending, is_websocket, has_error,
		 tags, agent, http_version, egress_ip, comment, secure, file_mtime, file_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			connection_id = excluded.connection_id,
			method = excluded.method,
			scheme = excluded.scheme,
			host = excluded.host,
			port = excluded.port,
			path = excluded.path,
			url = excluded.url,
			status = excluded.status,
			content_type = excluded.content_type,
			req_size = excluded.req_size,
			resp_size = excluded.resp_size,
			duration_ms = excluded.duration_ms,
			started_at = excluded.started_at,
			pending = excluded.pending,
			is_websocket = excluded.is_websocket,
			has_error = excluded.has_error,
			tags = excluded.tags,
			agent = excluded.agent,
			http_version = excluded.http_version,
			egress_ip = excluded.egress_ip,
			comment = excluded.comment,
			secure = excluded.secure,
			file_mtime = excluded.file_mtime,
			file_size = excluded.file_size
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range rows {
		tags, _ := json.Marshal(r.Tags)
		if _, err := stmt.ExecContext(ctx,
			r.ID, r.ConnectionID, r.Method, r.Scheme, r.Host, r.Port, r.Path, r.URL,
			r.Status, r.ContentType, r.ReqSize, r.RespSize, r.DurationMs, r.StartedAt,
			boolInt(r.Pending), boolInt(r.IsWebSocket), boolInt(r.HasError),
			string(tags), r.Agent, r.HTTPVersion, r.EgressIP, r.Comment, boolInt(r.Secure),
			r.FileMTime, r.FileSize); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (x *IndexStore) Query(ctx context.Context, f ExchangeFilter) ([]ExchangeRow, int64, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}

	where := []string{"1=1"}
	args := make([]any, 0, 12)

	if f.BeforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, f.BeforeID)
	}
	if f.AfterID > 0 {
		where = append(where, "id > ?")
		args = append(args, f.AfterID)
	}
	if f.Query != "" {
		q := "%" + f.Query + "%"
		where = append(where, "(url LIKE ? OR host LIKE ? OR path LIKE ?)")
		args = append(args, q, q, q)
	}
	if f.Method != "" {
		where = append(where, "method COLLATE NOCASE = ?")
		args = append(args, f.Method)
	}
	if f.Host != "" {
		where = append(where, "host = ?")
		args = append(args, f.Host)
	}
	if f.Status > 0 {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	switch f.StatusCls {
	case "2xx":
		where = append(where, "status >= 200 AND status < 300")
	case "3xx":
		where = append(where, "status >= 300 AND status < 400")
	case "4xx":
		where = append(where, "status >= 400 AND status < 500")
	case "5xx":
		where = append(where, "status >= 500")
	}
	if f.PendingOnly {
		where = append(where, "pending = 1")
	}
	if f.ErrorsOnly {
		where = append(where, "has_error = 1")
	}

	order := "id DESC"
	if f.AfterID > 0 {
		order = "id ASC"
	}

	query := `SELECT id, connection_id, method, scheme, host, port, path, url, status, content_type,
		req_size, resp_size, duration_ms, started_at, pending, is_websocket, has_error,
		tags, agent, http_version, egress_ip, comment, secure, file_mtime, file_size
		FROM exchanges WHERE ` + strings.Join(where, " AND ") + ` ORDER BY ` + order + ` LIMIT ?`
	args = append(args, f.Limit)

	rows, err := x.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]ExchangeRow, 0, f.Limit)
	for rows.Next() {
		r, err := scanExchange(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var lastID int64
	if len(out) > 0 {
		lastID = out[len(out)-1].ID
	}
	return out, lastID, nil
}

func (x *IndexStore) Get(ctx context.Context, id int64) (*ExchangeRow, error) {
	row := x.db.QueryRowContext(ctx,
		`SELECT id, connection_id, method, scheme, host, port, path, url, status, content_type,
		        req_size, resp_size, duration_ms, started_at, pending, is_websocket, has_error,
		        tags, agent, http_version, egress_ip, comment, secure, file_mtime, file_size
		 FROM exchanges WHERE id = ?`, id)
	r, err := scanExchange(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

func (x *IndexStore) GetFileMeta(ctx context.Context, id int64) (mtime, size int64, pending, found bool, err error) {
	err = x.db.QueryRowContext(ctx,
		`SELECT file_mtime, file_size, pending FROM exchanges WHERE id = ?`, id).Scan(&mtime, &size, &pending)
	if err == sql.ErrNoRows {
		return 0, 0, false, false, nil
	}
	if err != nil {
		return 0, 0, false, false, err
	}
	return mtime, size, pending, true, nil
}

// Totals returns the O(1) per-session counters maintained by SQLite triggers.
func (x *IndexStore) Totals(ctx context.Context) (Aggregate, int64, error) {
	var a Aggregate
	var pending int64
	err := x.db.QueryRowContext(ctx,
		`SELECT count, req_bytes, resp_bytes, s2, s3, s4, s5, errors, pending
		 FROM totals WHERE id = 1`).
		Scan(&a.Count, &a.ReqBytes, &a.RespBytes, &a.Status2xx, &a.Status3xx, &a.Status4xx, &a.Status5xx, &a.Errors, &pending)
	return a, pending, err
}

func (x *IndexStore) Count(ctx context.Context) (int64, error) {
	a, _, err := x.Totals(ctx)
	return a.Count, err
}

func (x *IndexStore) CountPending(ctx context.Context) (int64, error) {
	_, pending, err := x.Totals(ctx)
	return pending, err
}

func (x *IndexStore) AggregateStats(ctx context.Context) (Aggregate, error) {
	a, pending, err := x.Totals(ctx)
	a.Pending = pending
	return a, err
}

// SettledStats excludes pending exchanges. Aggregator.Reset uses this so a
// pending exchange that later finalizes is counted exactly once.
func (x *IndexStore) SettledStats(ctx context.Context) (Aggregate, error) {
	var a Aggregate
	err := x.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(req_size), 0), COALESCE(SUM(resp_size), 0),
		        COALESCE(SUM(CASE WHEN status >= 200 AND status < 300 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status >= 300 AND status < 400 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status >= 400 AND status < 500 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN has_error <> 0 THEN 1 ELSE 0 END), 0)
		 FROM exchanges WHERE pending = 0`).
		Scan(&a.Count, &a.ReqBytes, &a.RespBytes, &a.Status2xx, &a.Status3xx,
			&a.Status4xx, &a.Status5xx, &a.Errors)
	return a, err
}

// PendingIDs returns exchange IDs currently marked pending. It is used once
// after a full scan to seed the in-memory pending set, so status snapshots do
// not need to query SQLite on every refresh.
func (x *IndexStore) PendingIDs(ctx context.Context) ([]int64, error) {
	rows, err := x.db.QueryContext(ctx, `SELECT id FROM exchanges WHERE pending = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0, 32)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (x *IndexStore) SumBytes(ctx context.Context) (int64, error) {
	a, _, err := x.Totals(ctx)
	if err != nil {
		return 0, err
	}
	return a.ReqBytes + a.RespBytes, nil
}

func (x *IndexStore) MaxID(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := x.db.QueryRowContext(ctx, `SELECT MAX(id) FROM exchanges`).Scan(&n)
	return n.Int64, err
}

func (x *IndexStore) TopHosts(ctx context.Context, limit int) ([]HostCount, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := x.db.QueryContext(ctx,
		`SELECT host, count FROM host_counts ORDER BY count DESC, host ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostCount
	for rows.Next() {
		var h HostCount
		if err := rows.Scan(&h.Host, &h.Count); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

type HostCount struct {
	Host  string `json:"host"`
	Count int64  `json:"count"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExchange(s rowScanner) (ExchangeRow, error) {
	var r ExchangeRow
	var pending, ws, hasErr, secure int
	var tags string
	err := s.Scan(&r.ID, &r.ConnectionID, &r.Method, &r.Scheme, &r.Host, &r.Port,
		&r.Path, &r.URL, &r.Status, &r.ContentType, &r.ReqSize, &r.RespSize,
		&r.DurationMs, &r.StartedAt, &pending, &ws, &hasErr, &tags, &r.Agent,
		&r.HTTPVersion, &r.EgressIP, &r.Comment, &secure, &r.FileMTime, &r.FileSize)
	if err != nil {
		return r, err
	}
	r.Pending = pending != 0
	r.IsWebSocket = ws != 0
	r.HasError = hasErr != 0
	r.Secure = secure != 0
	if tags != "" {
		_ = json.Unmarshal([]byte(tags), &r.Tags)
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return r, nil
}

func SanitizeSessionID(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, id)
}

func ParseExchangeIDFromPath(path string) (int64, bool) {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "ex-") || !strings.HasSuffix(base, ".mpack") {
		return 0, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(base, "ex-"), ".mpack")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
