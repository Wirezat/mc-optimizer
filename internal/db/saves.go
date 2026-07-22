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

// CreateSave inserts a new save for the given user.
func (d *DB) CreateSave(ctx context.Context, userID uuid.UUID, name string) (*model.Save, error) {
	s := &model.Save{ID: uuid.New(), UserID: userID, Name: name, CreatedAt: time.Now().UTC()}
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO saves (id, user_id, name, created_at) VALUES ($1, $2, $3, $4)`,
		s.ID, s.UserID, s.Name, s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("db: create save: %w", err)
	}
	return s, nil
}

// ListSavesByUser returns all saves owned by userID, newest first.
func (d *DB) ListSavesByUser(ctx context.Context, userID uuid.UUID) ([]*model.Save, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT id, user_id, name, created_at,
		        (SELECT COUNT(*) FROM factories WHERE save_id = saves.id) AS factory_count,
		        (SELECT COUNT(*) FROM save_active_mods WHERE save_id = saves.id) AS mod_count
		 FROM saves WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: list saves: %w", err)
	}
	defer rows.Close()

	var saves []*model.Save
	for rows.Next() {
		s := &model.Save{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.Name, &s.CreatedAt, &s.FactoryCount, &s.ModCount); err != nil {
			return nil, fmt.Errorf("db: scan save: %w", err)
		}
		saves = append(saves, s)
	}
	return saves, rows.Err()
}

// GetSave fetches a save by ID, validates ownership; returns ErrNotFound if missing or unauthorized.
func (d *DB) GetSave(ctx context.Context, id, userID uuid.UUID) (*model.Save, error) {
	s := &model.Save{}
	err := d.Pool.QueryRow(ctx,
		`SELECT id, user_id, name, created_at,
		        (SELECT COUNT(*) FROM factories WHERE save_id = saves.id) AS factory_count,
		        (SELECT COUNT(*) FROM save_active_mods WHERE save_id = saves.id) AS mod_count
		 FROM saves WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&s.ID, &s.UserID, &s.Name, &s.CreatedAt, &s.FactoryCount, &s.ModCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get save: %w", err)
	}
	return s, nil
}

// DeleteSave removes a save owned by userID; cascades to child entities.
func (d *DB) DeleteSave(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM saves WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("db: delete save: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
