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

const (
	TokenTypeSession = "session"
	TokenTypeRefresh = "refresh"
)

// CreateToken inserts a new token record.
func (d *DB) CreateToken(ctx context.Context, userID uuid.UUID, tokenHash, tokenType string, expiresAt time.Time) (*model.Token, error) {
	t := &model.Token{ID: uuid.New(), UserID: userID, TokenHash: tokenHash, Type: tokenType, ExpiresAt: expiresAt}
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO tokens (id, user_id, token_hash, type, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		t.ID, t.UserID, t.TokenHash, t.Type, t.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("db: create token: %w", err)
	}
	return t, nil
}

// GetTokenByHash looks up a non-expired token by hash; returns ErrNotFound if missing or
// expired.
func (d *DB) GetTokenByHash(ctx context.Context, tokenHash string) (*model.Token, error) {
	t := &model.Token{}
	err := d.Pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, type, expires_at FROM tokens WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()`,
		tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.Type, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get token by hash: %w", err)
	}
	return t, nil
}

// ClaimRefreshToken marks a live, unused refresh token as used and returns it; returns
// ErrNotFound if no such token exists.
func (d *DB) ClaimRefreshToken(ctx context.Context, tokenHash string) (*model.Token, error) {
	t := &model.Token{}
	err := d.Pool.QueryRow(ctx,
		`UPDATE tokens SET used_at = now()
		 WHERE token_hash = $1 AND type = $2 AND used_at IS NULL AND expires_at > now()
		 RETURNING id, user_id, token_hash, type, expires_at`,
		tokenHash, TokenTypeRefresh,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.Type, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: claim refresh token: %w", err)
	}
	return t, nil
}

// UsedRefreshToken returns the owner and use time of an already used, unexpired refresh
// token; returns ErrNotFound if the hash is not such a token.
func (d *DB) UsedRefreshToken(ctx context.Context, tokenHash string) (uuid.UUID, time.Time, error) {
	var userID uuid.UUID
	var usedAt time.Time
	err := d.Pool.QueryRow(ctx,
		`SELECT user_id, used_at FROM tokens
		 WHERE token_hash = $1 AND type = $2 AND used_at IS NOT NULL AND expires_at > now()`,
		tokenHash, TokenTypeRefresh,
	).Scan(&userID, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("db: used refresh token: %w", err)
	}
	return userID, usedAt, nil
}

// DeleteToken removes a single token by hash.
func (d *DB) DeleteToken(ctx context.Context, tokenHash string) error {
	_, err := d.Pool.Exec(ctx, `DELETE FROM tokens WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("db: delete token: %w", err)
	}
	return nil
}

// DeleteAllTokensForUser removes all tokens for a user ("logout everywhere").
func (d *DB) DeleteAllTokensForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := d.Pool.Exec(ctx, `DELETE FROM tokens WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("db: delete all tokens for user: %w", err)
	}
	return nil
}

// DeleteExpiredTokens purges past-expiry tokens; returns count deleted.
func (d *DB) DeleteExpiredTokens(ctx context.Context) (int64, error) {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM tokens WHERE expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("db: delete expired tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}
