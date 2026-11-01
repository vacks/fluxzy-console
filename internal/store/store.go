package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrLegacySchema = errors.New("legacy fluxzy-console database detected; initialize fresh data volumes")

const schemaVersion = 1

// Store is the console's global SQLite database. Per-capture-session exchange
// indexes live in separate databases (see index.go) so retention can delete a
// whole session without vacuuming a giant table.
type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Path() string { return s.path }

// now returns Unix milliseconds. All user-facing timestamps (users, audit,
// config versions, sessions) use milliseconds; aggregate buckets are the one
// exception and stay in seconds because they represent minute buckets.
func now() int64 { return time.Now().UnixMilli() }

func (s *Store) migrate(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('schema_meta','users','capture_sessions')`)
	if err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}
	var hasMeta, hasLegacy bool
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		switch name {
		case "schema_meta":
			hasMeta = true
		case "users", "capture_sessions":
			hasLegacy = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasMeta && hasLegacy {
		return ErrLegacySchema
	}
	if hasMeta {
		var version int
		err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_meta ORDER BY version DESC LIMIT 1`).Scan(&version)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read schema version: %w", err)
		}
		if err == nil && version != schemaVersion {
			return fmt.Errorf("unsupported fluxzy-console schema version %d (want %d)", version, schemaVersion)
		}
		if err == nil && version == schemaVersion {
			return s.ensureIndexes(ctx)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_meta (version INTEGER PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'admin',
			disabled INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			last_login_at INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			csrf_token TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS capture_sessions (
			id TEXT PRIMARY KEY,
			dir TEXT NOT NULL,
			capture_date INTEGER NOT NULL,
			fluxzy_version TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'indexing',
			exchange_count INTEGER NOT NULL DEFAULT 0,
			total_bytes INTEGER NOT NULL DEFAULT 0,
			last_indexed_at INTEGER NOT NULL DEFAULT 0,
			meta_json TEXT NOT NULL DEFAULT '',
			rules_hash TEXT NOT NULL DEFAULT '',
			config_hash TEXT NOT NULL DEFAULT '',
			rotation_reason TEXT NOT NULL DEFAULT '',
			index_error TEXT NOT NULL DEFAULT '',
			indexing_started_at INTEGER NOT NULL DEFAULT 0,
			indexed_at INTEGER NOT NULL DEFAULT 0,
			dir_bytes INTEGER NOT NULL DEFAULT 0,
			pinned INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_capture_sessions_date ON capture_sessions(capture_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_capture_sessions_status_date ON capture_sessions(status, capture_date DESC)`,
		`CREATE TABLE IF NOT EXISTS config_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			yaml TEXT NOT NULL,
			yaml_hash TEXT NOT NULL,
			author_id INTEGER,
			author_name TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			validation_status TEXT NOT NULL DEFAULT 'unknown',
			validation_error TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			active_session_id TEXT NOT NULL DEFAULT '',
			applied_at INTEGER NOT NULL DEFAULT 0,
			applied_session_id TEXT NOT NULL DEFAULT '',
			applied_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_config_versions_created ON config_versions(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			username TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			target TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS aggregates (
			bucket INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			count INTEGER NOT NULL DEFAULT 0,
			req_bytes INTEGER NOT NULL DEFAULT 0,
			resp_bytes INTEGER NOT NULL DEFAULT 0,
			status_2xx INTEGER NOT NULL DEFAULT 0,
			status_3xx INTEGER NOT NULL DEFAULT 0,
			status_4xx INTEGER NOT NULL DEFAULT 0,
			status_5xx INTEGER NOT NULL DEFAULT 0,
			errors INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (bucket, session_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aggregates_bucket ON aggregates(bucket DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_aggregates_session ON aggregates(session_id, bucket)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS index_errors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			file TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_index_errors_session ON index_errors(session_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS fluxzy_runtime_status (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			state TEXT NOT NULL DEFAULT 'stopped',
			pid INTEGER NOT NULL DEFAULT 0,
			active_session_id TEXT NOT NULL DEFAULT '',
			binary_version TEXT NOT NULL DEFAULT '',
			binary_release TEXT NOT NULL DEFAULT '',
			binary_path TEXT NOT NULL DEFAULT '',
			started_at INTEGER NOT NULL DEFAULT 0,
			last_exit_code INTEGER,
			last_error TEXT NOT NULL DEFAULT '',
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS fluxzy_supervisor_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			pid INTEGER NOT NULL DEFAULT 0,
			exit_code INTEGER,
			message TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_supervisor_events_created ON fluxzy_supervisor_events(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS fluxzy_runtime_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			listen_addr TEXT NOT NULL,
			mode TEXT NOT NULL,
			reverse_port INTEGER NOT NULL DEFAULT 0,
			max_upstream_connections INTEGER NOT NULL DEFAULT 16,
			request_buffer INTEGER NOT NULL DEFAULT 4096,
			max_capture_count INTEGER NOT NULL DEFAULT 0,
			serve_h2 INTEGER NOT NULL DEFAULT 0,
			parse_ua INTEGER NOT NULL DEFAULT 0,
			no_cert_cache INTEGER NOT NULL DEFAULT 0,
			skip_ssl_decryption INTEGER NOT NULL DEFAULT 0,
			config_hash TEXT NOT NULL,
			config_json TEXT NOT NULL,
			applied_hash TEXT NOT NULL DEFAULT '',
			applied_session_id TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL,
			updated_by TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS fluxzy_config_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			config_hash TEXT NOT NULL,
			config_json TEXT NOT NULL,
			author TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_config_history_created ON fluxzy_config_history(created_at DESC)`,
		`INSERT INTO schema_meta(version) SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM schema_meta)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w\n%s", err, stmt)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.ensureIndexes(ctx)
}

// ensureIndexes is idempotent and also runs for databases already at the
// current schema version, so adding indexes does not require a schema bump.
func (s *Store) ensureIndexes(ctx context.Context) error {
	for _, stmt := range []string{
		`DELETE FROM schema_meta WHERE rowid NOT IN (SELECT MAX(rowid) FROM schema_meta)`,
		`CREATE INDEX IF NOT EXISTS idx_config_versions_hash ON config_versions(yaml_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_config_history_hash ON fluxzy_config_history(config_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action, created_at DESC)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure index: %w\n%s", err, stmt)
		}
	}
	return nil
}
