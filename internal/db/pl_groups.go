package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (d *DB) CreatePLGroup(ctx context.Context, factoryID uuid.UUID, name string, position string) (*model.PLGroup, error) {
	g := &model.PLGroup{ID: uuid.New(), FactoryID: factoryID, Name: name, Position: position}
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO pl_groups (id, factory_id, name, position) VALUES ($1, $2, $3, $4)`,
		g.ID, g.FactoryID, g.Name, g.Position,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create pl group: %w", err)
	}
	return g, nil
}

func (d *DB) ListPLGroupsByFactory(ctx context.Context, factoryID uuid.UUID) ([]*model.PLGroup, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT id, factory_id, name, position FROM pl_groups WHERE factory_id = $1 ORDER BY position`,
		factoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: list pl groups: %w", err)
	}
	defer rows.Close()

	var groups []*model.PLGroup
	for rows.Next() {
		g := &model.PLGroup{}
		if err := rows.Scan(&g.ID, &g.FactoryID, &g.Name, &g.Position); err != nil {
			return nil, fmt.Errorf("db: scan pl group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (d *DB) RenamePLGroup(ctx context.Context, id uuid.UUID, name string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE pl_groups SET name = $2 WHERE id = $1`,
		id, name,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("db: rename pl group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) DeletePLGroup(ctx context.Context, id uuid.UUID) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: delete pl group: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete all production lines that belong to this group
	if _, err := tx.Exec(ctx, `DELETE FROM production_lines WHERE pl_group_id = $1`, id); err != nil {
		return fmt.Errorf("db: delete pl group: delete lines: %w", err)
	}

	tag, err := tx.Exec(ctx, `DELETE FROM pl_groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("db: delete pl group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// GetGroupPosition returns the current position key of a PL group.
func (d *DB) GetGroupPosition(ctx context.Context, id uuid.UUID) (string, error) {
	var pos string
	err := d.Pool.QueryRow(ctx, `SELECT position FROM pl_groups WHERE id = $1`, id).Scan(&pos)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("db: get group position: %w", err)
	}
	return pos, nil
}

// SetGroupPosition updates the position key of a PL group.
func (d *DB) SetGroupPosition(ctx context.Context, id uuid.UUID, position string) error {
	tag, err := d.Pool.Exec(ctx,
		`UPDATE pl_groups SET position = $2 WHERE id = $1`,
		id, position,
	)
	if err != nil {
		return fmt.Errorf("db: set group position: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetLastGroupPosition returns the highest position key among all groups in a factory, or
// "" if none exist (use fracidx.Between("", "") for the first).
func (d *DB) GetLastGroupPosition(ctx context.Context, factoryID uuid.UUID) (string, error) {
	var pos string
	err := d.Pool.QueryRow(ctx,
		`SELECT position FROM pl_groups WHERE factory_id = $1 ORDER BY position DESC LIMIT 1`,
		factoryID,
	).Scan(&pos)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("db: get last group position: %w", err)
	}
	return pos, nil
}

func (d *DB) PLGroupOwnerUserID(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := d.Pool.QueryRow(ctx, `
		SELECT s.user_id FROM pl_groups g
		JOIN factories f ON f.id = g.factory_id
		JOIN saves s ON s.id = f.save_id
		WHERE g.id = $1
	`, groupID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: pl group owner: %w", err)
	}
	return userID, nil
}
