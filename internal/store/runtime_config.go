package store

import (
	"context"
	"database/sql"
	"errors"
)

type RuntimeConfig struct {
	ListenAddr             string
	Mode                   string
	ReversePort            int
	MaxUpstreamConnections int
	RequestBuffer          int
	MaxCaptureCount        int64
	ServeH2                bool
	ParseUA                bool
	NoCertCache            bool
	SkipSSLDecryption      bool
	ConfigHash             string
	ConfigJSON             string
	AppliedHash            string
	AppliedSessionID       string
	UpdatedAt              int64
	UpdatedBy              string
}

func (s *Store) GetRuntimeConfig(ctx context.Context) (*RuntimeConfig, error) {
	var c RuntimeConfig
	var serveH2, parseUA, noCertCache, skipSSL int
	err := s.db.QueryRowContext(ctx,
		`SELECT listen_addr, mode, reverse_port, max_upstream_connections, request_buffer,
		        max_capture_count, serve_h2, parse_ua, no_cert_cache, skip_ssl_decryption,
		        config_hash, config_json, applied_hash, applied_session_id, updated_at, updated_by
		 FROM fluxzy_runtime_config WHERE id = 1`).
		Scan(&c.ListenAddr, &c.Mode, &c.ReversePort, &c.MaxUpstreamConnections,
			&c.RequestBuffer, &c.MaxCaptureCount, &serveH2, &parseUA, &noCertCache,
			&skipSSL, &c.ConfigHash, &c.ConfigJSON, &c.AppliedHash, &c.AppliedSessionID,
			&c.UpdatedAt, &c.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.ServeH2 = serveH2 != 0
	c.ParseUA = parseUA != 0
	c.NoCertCache = noCertCache != 0
	c.SkipSSLDecryption = skipSSL != 0
	return &c, nil
}

func (s *Store) SaveRuntimeConfig(ctx context.Context, c RuntimeConfig, author string) error {
	ts := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO fluxzy_runtime_config
		 (id, listen_addr, mode, reverse_port, max_upstream_connections, request_buffer,
		  max_capture_count, serve_h2, parse_ua, no_cert_cache, skip_ssl_decryption,
		  config_hash, config_json, applied_hash, applied_session_id, updated_at, updated_by)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   listen_addr = excluded.listen_addr,
		   mode = excluded.mode,
		   reverse_port = excluded.reverse_port,
		   max_upstream_connections = excluded.max_upstream_connections,
		   request_buffer = excluded.request_buffer,
		   max_capture_count = excluded.max_capture_count,
		   serve_h2 = excluded.serve_h2,
		   parse_ua = excluded.parse_ua,
		   no_cert_cache = excluded.no_cert_cache,
		   skip_ssl_decryption = excluded.skip_ssl_decryption,
		   config_hash = excluded.config_hash,
		   config_json = excluded.config_json,
		   updated_at = excluded.updated_at,
		   updated_by = excluded.updated_by`,
		c.ListenAddr, c.Mode, c.ReversePort, c.MaxUpstreamConnections, c.RequestBuffer,
		c.MaxCaptureCount, boolInt(c.ServeH2), boolInt(c.ParseUA), boolInt(c.NoCertCache),
		boolInt(c.SkipSSLDecryption), c.ConfigHash, c.ConfigJSON, c.AppliedHash,
		c.AppliedSessionID, ts, author); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO fluxzy_config_history (config_hash, config_json, author, created_at)
		 VALUES (?, ?, ?, ?)`, c.ConfigHash, c.ConfigJSON, author, ts); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkRuntimeConfigApplied(ctx context.Context, hash, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE fluxzy_runtime_config SET applied_hash = ?, applied_session_id = ? WHERE id = 1`,
		hash, sessionID)
	return err
}

type RuntimeConfigHistory struct {
	ID         int64
	ConfigHash string
	ConfigJSON string
	Author     string
	CreatedAt  int64
}

func (s *Store) GetRuntimeConfigHistoryByHash(ctx context.Context, hash string) (*RuntimeConfigHistory, error) {
	var h RuntimeConfigHistory
	err := s.db.QueryRowContext(ctx,
		`SELECT id, config_hash, config_json, author, created_at
		 FROM fluxzy_config_history WHERE config_hash = ? ORDER BY id DESC LIMIT 1`, hash).
		Scan(&h.ID, &h.ConfigHash, &h.ConfigJSON, &h.Author, &h.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *Store) ListRuntimeConfigHistory(ctx context.Context, limit int) ([]RuntimeConfigHistory, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, config_hash, config_json, author, created_at
		 FROM fluxzy_config_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RuntimeConfigHistory, 0, limit)
	for rows.Next() {
		var h RuntimeConfigHistory
		if err := rows.Scan(&h.ID, &h.ConfigHash, &h.ConfigJSON, &h.Author, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
