package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateFactory inserts a new factory inside a save.
func (d *DB) CreateFactory(ctx context.Context, saveID uuid.UUID, name string, src bool) (*model.Factory, error) {
	f := &model.Factory{ID: uuid.New(), SaveID: saveID, Name: name, Src: src}
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO factories (id, save_id, name, src) VALUES ($1, $2, $3, $4)`,
		f.ID, f.SaveID, f.Name, f.Src,
	)
	if err != nil {
		return nil, fmt.Errorf("db: create factory: %w", err)
	}
	return f, nil
}

// ListFactoriesBySave returns all factories belonging to a save, ordered by name.
func (d *DB) ListFactoriesBySave(ctx context.Context, saveID uuid.UUID) ([]*model.Factory, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT id, save_id, name, src,
		        (SELECT COUNT(*) FROM production_lines WHERE factory_id = factories.id) AS pl_count
		 FROM factories WHERE save_id = $1 ORDER BY src, name`,
		saveID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: list factories: %w", err)
	}
	defer rows.Close()

	var factories []*model.Factory
	for rows.Next() {
		f := &model.Factory{}
		if err := rows.Scan(&f.ID, &f.SaveID, &f.Name, &f.Src, &f.PLCount); err != nil {
			return nil, fmt.Errorf("db: scan factory: %w", err)
		}
		factories = append(factories, f)
	}
	return factories, rows.Err()
}

// GetFactory fetches a single factory by ID.
func (d *DB) GetFactory(ctx context.Context, id uuid.UUID) (*model.Factory, error) {
	f := &model.Factory{}
	err := d.Pool.QueryRow(ctx,
		`SELECT id, save_id, name, src FROM factories WHERE id = $1`, id,
	).Scan(&f.ID, &f.SaveID, &f.Name, &f.Src)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get factory: %w", err)
	}
	return f, nil
}

// RenameFactory updates the display name of a factory.
func (d *DB) RenameFactory(ctx context.Context, id uuid.UUID, name string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE factories SET name = $2 WHERE id = $1`,
		id, name,
	)
	if err != nil {
		return fmt.Errorf("db: rename factory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetFactorySrc toggles the source-factory flag.
func (d *DB) SetFactorySrc(ctx context.Context, id uuid.UUID, src bool) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE factories SET src = $2 WHERE id = $1`,
		id, src,
	)
	if err != nil {
		return fmt.Errorf("db: set factory src: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
