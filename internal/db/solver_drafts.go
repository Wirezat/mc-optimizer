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

// CreateSolverDraft stores a solver result for a factory, expiring in 24 hours.
func (d *DB) CreateSolverDraft(ctx context.Context, factoryID, userID uuid.UUID, result []byte) (*model.SolverDraft, error) {
	now := time.Now().UTC()
	draft := &model.SolverDraft{
		ID:        uuid.New(),
		FactoryID: factoryID,
		UserID:    userID,
		Result:    result,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO solver_drafts (id, factory_id, user_id, result, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, draft.ID, draft.FactoryID, draft.UserID, draft.Result, draft.ExpiresAt, draft.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("db: create solver draft: %w", err)
	}
	return draft, nil
}

// GetSolverDraft fetches a non-expired draft by ID; returns ErrNotFound if missing or expired.
func (d *DB) GetSolverDraft(ctx context.Context, id uuid.UUID) (*model.SolverDraft, error) {
	draft := &model.SolverDraft{}
	err := d.Pool.QueryRow(ctx, `
		SELECT id, factory_id, user_id, result, expires_at, created_at
		FROM solver_drafts
		WHERE id = $1 AND expires_at > now()
	`, id).Scan(
		&draft.ID, &draft.FactoryID, &draft.UserID,
		&draft.Result, &draft.ExpiresAt, &draft.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get solver draft: %w", err)
	}
	return draft, nil
}

// DeleteExpiredSolverDrafts removes drafts past their expiry; returns the count deleted.
func (d *DB) DeleteExpiredSolverDrafts(ctx context.Context) (int64, error) {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM solver_drafts WHERE expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("db: delete expired solver drafts: %w", err)
	}
	return tag.RowsAffected(), nil
}

