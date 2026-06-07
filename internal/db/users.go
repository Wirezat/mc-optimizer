package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const userSelect = `SELECT id, username, is_admin, password_hash, created_at FROM users`

// scanUser scans a user row from pgx.Row and returns a model.User.
func scanUser(row pgx.Row) (*model.User, error) {
	u := &model.User{}
	if err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

// CreateUser inserts a new user; returns ErrConflict if username is taken.
func (d *DB) CreateUser(ctx context.Context, username, passwordHash string) (*model.User, error) {
	u := &model.User{ID: uuid.New(), Username: username, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO users (id, username, password_hash, created_at) VALUES ($1, $2, $3, $4)`,
		u.ID, u.Username, u.PasswordHash, u.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create user: %w", err)
	}
	return u, nil
}

// UpdateUsername sets a new username for the given user; returns ErrConflict if taken.
func (d *DB) UpdateUsername(ctx context.Context, userID uuid.UUID, newUsername string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE users SET username = $1 WHERE id = $2`, newUsername, userID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("db: update username: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePassword sets a new password hash for the given user.
func (d *DB) UpdatePassword(ctx context.Context, userID uuid.UUID, newHash string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`, newHash, userID)
	if err != nil {
		return fmt.Errorf("db: update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetUserByUsername fetches a user by username; returns ErrNotFound if absent.
func (d *DB) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	u, err := scanUser(d.Pool.QueryRow(ctx, userSelect+` WHERE username = $1`, username))
	if err != nil {
		return nil, fmt.Errorf("db: get user by username: %w", err)
	}
	return u, nil
}

// GetUserByID fetches a user by primary key; returns ErrNotFound if absent.
func (d *DB) GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	u, err := scanUser(d.Pool.QueryRow(ctx, userSelect+` WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("db: get user by id: %w", err)
	}
	return u, nil
}
