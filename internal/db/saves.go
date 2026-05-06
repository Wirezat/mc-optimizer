package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Save represents a row in the saves table.
type Save struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateSave inserts a new save for the given user.
func (d *DB) CreateSave(ctx context.Context, userID uuid.UUID, name string) (*Save, error) {
	s := &Save{ID: uuid.New(), UserID: userID, Name: name, CreatedAt: time.Now().UTC()}
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
func (d *DB) ListSavesByUser(ctx context.Context, userID uuid.UUID) ([]*Save, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT id, user_id, name, created_at FROM saves WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: list saves: %w", err)
	}
	defer rows.Close()

	var saves []*Save
	for rows.Next() {
		s := &Save{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.Name, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("db: scan save: %w", err)
		}
		saves = append(saves, s)
	}
	return saves, rows.Err()
}

// GetSave fetches a save by ID, validates ownership; returns ErrNotFound if missing or unauthorized.
func (d *DB) GetSave(ctx context.Context, id, userID uuid.UUID) (*Save, error) {
	s := &Save{}
	err := d.Pool.QueryRow(ctx,
		`SELECT id, user_id, name, created_at FROM saves WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&s.ID, &s.UserID, &s.Name, &s.CreatedAt)
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

// Factory represents a row in the factories table.
type Factory struct {
	ID     uuid.UUID `json:"id"`
	SaveID uuid.UUID `json:"save_id"`
	Name   string    `json:"name"`
}

// CreateFactory inserts a new factory inside a save.
func (d *DB) CreateFactory(ctx context.Context, saveID uuid.UUID, name string) (*Factory, error) {
	f := &Factory{ID: uuid.New(), SaveID: saveID, Name: name}
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO factories (id, save_id, name) VALUES ($1, $2, $3)`,
		f.ID, f.SaveID, f.Name,
	)
	if err != nil {
		return nil, fmt.Errorf("db: create factory: %w", err)
	}
	return f, nil
}

// ListFactoriesBySave returns all factories belonging to a save, ordered by name.
func (d *DB) ListFactoriesBySave(ctx context.Context, saveID uuid.UUID) ([]*Factory, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT id, save_id, name FROM factories WHERE save_id = $1 ORDER BY name`,
		saveID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: list factories: %w", err)
	}
	defer rows.Close()

	var factories []*Factory
	for rows.Next() {
		f := &Factory{}
		if err := rows.Scan(&f.ID, &f.SaveID, &f.Name); err != nil {
			return nil, fmt.Errorf("db: scan factory: %w", err)
		}
		factories = append(factories, f)
	}
	return factories, rows.Err()
}

// GetFactory fetches a single factory by ID; caller must authorize via parent save.
func (d *DB) GetFactory(ctx context.Context, id uuid.UUID) (*Factory, error) {
	f := &Factory{}
	err := d.Pool.QueryRow(ctx,
		`SELECT id, save_id, name FROM factories WHERE id = $1`, id,
	).Scan(&f.ID, &f.SaveID, &f.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get factory: %w", err)
	}
	return f, nil
}

// DeleteFactory removes a factory and cascades to child entities.
func (d *DB) DeleteFactory(ctx context.Context, id uuid.UUID) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM factories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("db: delete factory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FactoryOwnerUserID resolves the user_id that owns a factory via its parent save.
func (d *DB) FactoryOwnerUserID(ctx context.Context, factoryID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := d.Pool.QueryRow(ctx,
		`SELECT s.user_id FROM factories f JOIN saves s ON s.id = f.save_id WHERE f.id = $1`,
		factoryID,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: factory owner: %w", err)
	}
	return userID, nil
}
