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

const userSelect = `SELECT id, username, is_admin, is_owner, password_hash, created_at FROM users`

// scanUser scans a user row from pgx.Row and returns a model.User.
func scanUser(row pgx.Row) (*model.User, error) {
	u := &model.User{}
	if err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.IsOwner, &u.PasswordHash, &u.CreatedAt); err != nil {
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

// ListUsers returns all users ordered by created_at ascending.
func (d *DB) ListUsers(ctx context.Context) ([]*model.User, error) {
	rows, err := d.Pool.Query(ctx, userSelect+` ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("db: list users: %w", err)
	}
	defer rows.Close()
	var users []*model.User
	for rows.Next() {
		u := &model.User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.IsOwner, &u.PasswordHash, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("db: list users scan: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// SetUserAdmin updates the is_admin flag for a user by ID.
func (d *DB) SetUserAdmin(ctx context.Context, userID uuid.UUID, isAdmin bool) error {
	tag, err := d.Pool.Exec(ctx, `UPDATE users SET is_admin = $1 WHERE id = $2`, isAdmin, userID)
	if err != nil {
		return fmt.Errorf("db: set user admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TransferOwnership atomically moves is_owner from fromID to toID. Returns ErrNotFound if
// either user does not exist.
func (d *DB) TransferOwnership(ctx context.Context, fromID, toID uuid.UUID) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: transfer ownership begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if tag, err := tx.Exec(ctx, `UPDATE users SET is_owner = FALSE WHERE id = $1`, fromID); err != nil {
		return fmt.Errorf("db: transfer ownership revoke: %w", err)
	} else if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if tag, err := tx.Exec(ctx, `UPDATE users SET is_owner = TRUE, is_admin = TRUE WHERE id = $1`, toID); err != nil {
		return fmt.Errorf("db: transfer ownership grant: %w", err)
	} else if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Invalidate old owner's sessions so their cached isOwner flag expires immediately.
	_ = d.DeleteAllTokensForUser(ctx, fromID)
	return nil
}

// DeleteUser removes a user by ID. Returns ErrNotFound if the user does not exist.
func (d *DB) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("db: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// HasOwner returns true if any user with is_owner = TRUE exists.
func (d *DB) HasOwner(ctx context.Context) (bool, error) {
	var exists bool
	err := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_owner = TRUE)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("db: has owner: %w", err)
	}
	return exists, nil
}

// PromoteToOwnerIfFirst sets is_owner = TRUE and is_admin = TRUE for the given user if no
// owner exists yet. Returns true if the promotion happened.
func (d *DB) PromoteToOwnerIfFirst(ctx context.Context, userID uuid.UUID) (bool, error) {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE users SET is_owner = TRUE, is_admin = TRUE
		WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM users WHERE is_owner = TRUE)`,
		userID,
	)
	if err != nil {
		return false, fmt.Errorf("db: promote to owner: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
