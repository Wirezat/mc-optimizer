package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
)

// ReplaceEnergies makes defs the energy forms of modID: it writes each one and deletes the
// mod's forms that defs no longer lists, in one transaction.
func (d *DB) ReplaceEnergies(ctx context.Context, modID string, defs []model.EnergyDef) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: replace energies %s: begin: %w", modID, err)
	}
	defer tx.Rollback(ctx)

	keep := make([]string, 0, len(defs))
	for _, e := range defs {
		if e.ModID != modID {
			return fmt.Errorf("db: replace energies %s: %s:%s belongs to another mod", modID, e.ModID, e.EnergyID)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO energies (mod_id, energy_id, symbol, lang_key, fe_per_unit_num, fe_per_unit_den)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)
			ON CONFLICT (mod_id, energy_id) DO UPDATE SET
				symbol = EXCLUDED.symbol, lang_key = EXCLUDED.lang_key,
				fe_per_unit_num = EXCLUDED.fe_per_unit_num, fe_per_unit_den = EXCLUDED.fe_per_unit_den
		`, modID, e.EnergyID, e.Symbol, e.LangKey, e.FePerUnit.Num, e.FePerUnit.Den); err != nil {
			return fmt.Errorf("db: replace energies %s: upsert %s: %w", modID, e.EnergyID, err)
		}
		keep = append(keep, e.EnergyID)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM energies WHERE mod_id = $1 AND NOT (energy_id = ANY($2))`, modID, keep); err != nil {
		return fmt.Errorf("db: replace energies %s: delete dropped: %w", modID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: replace energies %s: commit: %w", modID, err)
	}
	return nil
}

// ListAllEnergies returns every energy form, or with saveID only those of the save's active
// mods; Name is the en_us translation of the lang_key, else the symbol.
func (d *DB) ListAllEnergies(ctx context.Context, saveID *uuid.UUID) ([]*model.Energy, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT e.mod_id, e.energy_id, e.symbol,
		       COALESCE((SELECT t.name FROM translations t WHERE t.lang = 'en_us' AND t.lang_key = e.lang_key), e.symbol),
		       e.fe_per_unit_num, e.fe_per_unit_den
		FROM energies e
		WHERE $1::uuid IS NULL OR e.mod_id IN (SELECT mod_id FROM save_active_mods WHERE save_id = $1)
		ORDER BY e.mod_id, e.energy_id
	`, saveID)
	if err != nil {
		return nil, fmt.Errorf("db: list all energies: %w", err)
	}
	defer rows.Close()

	var out []*model.Energy
	for rows.Next() {
		e := &model.Energy{}
		if err := rows.Scan(&e.ModID, &e.EnergyID, &e.Symbol, &e.Name, &e.FePerUnit.Num, &e.FePerUnit.Den); err != nil {
			return nil, fmt.Errorf("db: scan energy: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
