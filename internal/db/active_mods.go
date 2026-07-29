package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
)

// GetActiveMods returns the set of mod IDs active for a save. An empty (but
// non-nil) map means the save has no active mods configured — callers must
// not treat that as "unrestricted"; that distinction belongs to the caller.
func (d *DB) GetActiveMods(ctx context.Context, saveID uuid.UUID) (map[string]bool, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT mod_id FROM save_active_mods WHERE save_id = $1`, saveID)
	if err != nil {
		return nil, fmt.Errorf("db: get active mods: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var modID string
		if err := rows.Scan(&modID); err != nil {
			return nil, fmt.Errorf("db: scan active mod: %w", err)
		}
		out[modID] = true
	}
	return out, rows.Err()
}

// GetDependentProductionLines returns every production line in the given save
// (any status) that has at least one machine group belonging to one of modIDs
// — i.e. lines that would be affected by deactivating those mods. Used to
// warn the user before saving a narrower active-mod selection.
func (d *DB) GetDependentProductionLines(ctx context.Context, saveID uuid.UUID, modIDs []string) ([]*model.DependentProductionLine, error) {
	if len(modIDs) == 0 {
		return nil, nil
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT DISTINCT pl.id,
		       COALESCE(
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||pl.target_mod_id||'.'||pl.target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||pl.target_mod_id||'.'||pl.target_item_id),
		         (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||pl.target_mod_id||'.'||pl.target_item_id),
		         ''
		       ) AS target_item_name,
		       f.id, f.name
		FROM production_lines pl
		JOIN factories f ON f.id = pl.factory_id
		JOIN machine_groups mg ON mg.pl_id = pl.id
		WHERE f.save_id = $1 AND mg.machine_mod_id = ANY($2)
		ORDER BY f.name, target_item_name
	`, saveID, modIDs)
	if err != nil {
		return nil, fmt.Errorf("db: get dependent production lines: %w", err)
	}
	defer rows.Close()

	var out []*model.DependentProductionLine
	for rows.Next() {
		dpl := &model.DependentProductionLine{}
		if err := rows.Scan(&dpl.ID, &dpl.TargetItemName, &dpl.FactoryID, &dpl.FactoryName); err != nil {
			return nil, fmt.Errorf("db: scan dependent production line: %w", err)
		}
		out = append(out, dpl)
	}
	return out, rows.Err()
}

// SetActiveMods replaces the full active-mod set for a save in one transaction.
func (d *DB) SetActiveMods(ctx context.Context, saveID uuid.UUID, modIDs []string) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: set active mods: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM save_active_mods WHERE save_id = $1`, saveID); err != nil {
		return fmt.Errorf("db: set active mods: delete: %w", err)
	}
	for _, modID := range modIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO save_active_mods (save_id, mod_id) VALUES ($1, $2)`,
			saveID, modID); err != nil {
			return fmt.Errorf("db: set active mods: insert %s: %w", modID, err)
		}
	}
	return tx.Commit(ctx)
}
