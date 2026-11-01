package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("not found")
var ErrUsernameExists = errors.New("username already exists")

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	Disabled     bool   `json:"disabled"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
	LastLoginAt  *int64 `json:"lastLoginAt,omitempty"`
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (*User, error) {
	ts := now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, disabled, created_at, updated_at)
		 VALUES (?, ?, 'admin', 0, ?, ?)`,
		username, passwordHash, ts, ts)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.username") {
			return nil, ErrUsernameExists
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.GetUserByID(ctx, id)
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	var disabled int
	var last sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, disabled, created_at, updated_at, last_login_at
		 FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &disabled, &u.CreatedAt, &u.UpdatedAt, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Disabled = disabled != 0
	if last.Valid {
		u.LastLoginAt = &last.Int64
	}
	return u, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	var disabled int
	var last sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, disabled, created_at, updated_at, last_login_at
		 FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &disabled, &u.CreatedAt, &u.UpdatedAt, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Disabled = disabled != 0
	if last.Valid {
		u.LastLoginAt = &last.Int64
	}
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, role, disabled, created_at, updated_at, last_login_at
		 FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		var disabled int
		var last sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &disabled, &u.CreatedAt, &u.UpdatedAt, &last); err != nil {
			return nil, err
		}
		u.Disabled = disabled != 0
		if last.Valid {
			u.LastLoginAt = &last.Int64
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, id int64, disabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET role = 'admin', disabled = ?, updated_at = ? WHERE id = ?`,
		boolInt(disabled), now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, hash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) TouchLastLogin(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now(), id)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
