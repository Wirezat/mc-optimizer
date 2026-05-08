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

var ErrNotFound = errors.New("db: record not found")
var ErrConflict = errors.New("db: unique constraint violation")

const userSelect = `SELECT id, username, email, password_hash, created_at FROM users`

func scanUser(row pgx.Row) (*model.User, error) {
	u := &model.User{}
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

// CreateUser inserts a new user; returns ErrConflict if username or email taken.
func (d *DB) CreateUser(ctx context.Context, username, email, passwordHash string) (*model.User, error) {
	u := &model.User{ID: uuid.New(), Username: username, Email: email, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO users (id, username, email, password_hash, created_at) VALUES ($1, $2, $3, $4, $5)`,
		u.ID, u.Username, u.Email, u.PasswordHash, u.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create user: %w", err)
	}
	return u, nil
}

// GetUserByEmail fetches a user by email; returns ErrNotFound if absent.
func (d *DB) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	u, err := scanUser(d.Pool.QueryRow(ctx, userSelect+` WHERE email = $1`, email))
	if err != nil {
		return nil, fmt.Errorf("db: get user by email: %w", err)
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
