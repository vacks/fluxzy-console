package store

import (
	"context"
	"database/sql"
	"errors"
)

type AuthSession struct {
	ID         int64  `json:"-"`
	UserID     int64  `json:"-"`
	TokenHash  string `json:"-"`
	CSRFToken  string `json:"-"`
	CreatedAt  int64  `json:"createdAt"`
	ExpiresAt  int64  `json:"expiresAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	IP         string `json:"-"`
	UserAgent  string `json:"-"`
}

func (s *Store) CreateAuthSession(ctx context.Context, userID int64, tokenHash, csrfToken string, expiresAt int64, ip, ua string) error {
	ts := now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_sessions (user_id, token_hash, csrf_token, created_at, expires_at, last_seen_at, ip, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, tokenHash, csrfToken, ts, expiresAt, ts, ip, ua)
	return err
}

func (s *Store) GetAuthSession(ctx context.Context, tokenHash string) (*AuthSession, *User, error) {
	as := &AuthSession{}
	u := &User{}
	var disabled int
	var lastLogin sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT a.id, a.user_id, a.token_hash, a.csrf_token, a.created_at, a.expires_at, a.last_seen_at, a.ip, a.user_agent,
		        u.id, u.username, u.password_hash, u.role, u.disabled, u.created_at, u.updated_at, u.last_login_at
		 FROM auth_sessions a JOIN users u ON u.id = a.user_id
		 WHERE a.token_hash = ? AND a.expires_at > ?`, tokenHash, now()).
		Scan(&as.ID, &as.UserID, &as.TokenHash, &as.CSRFToken, &as.CreatedAt, &as.ExpiresAt, &as.LastSeenAt, &as.IP, &as.UserAgent,
			&u.ID, &u.Username, &u.PasswordHash, &u.Role, &disabled, &u.CreatedAt, &u.UpdatedAt, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	u.Disabled = disabled != 0
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Int64
	}
	return as, u, nil
}

func (s *Store) TouchAuthSession(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE auth_sessions SET last_seen_at = ? WHERE id = ?`, now(), id)
}

func (s *Store) DeleteAuthSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteUserAuthSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) DeleteExpiredAuthSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, now())
	return err
}
