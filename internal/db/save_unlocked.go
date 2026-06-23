package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

// GetUnlockedUpgradeTiers returns the upgrade tiers the user has unlocked for a given save and mod,
// cheapest (lowest EU bonus) first.
func (d *DB) GetUnlockedUpgradeTiers(ctx context.Context, saveID uuid.UUID, modID string) ([]*solver.UpgradeTierSpec, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT ut.id, ut.eu_bonus_per_slot
		FROM upgrade_tiers ut
		JOIN save_unlocked_items sui
		  ON sui.mod_id = ut.mod_id
		 AND sui.item_id = ut.name
		 AND sui.save_id = $1
		WHERE ut.mod_id = $2
		ORDER BY ut.eu_bonus_per_slot ASC
	`, saveID, modID)
	if err != nil {
		return nil, fmt.Errorf("db: get unlocked upgrade tiers: %w", err)
	}
	defer rows.Close()

	var tiers []*solver.UpgradeTierSpec
	for rows.Next() {
		t := &solver.UpgradeTierSpec{}
		if err := rows.Scan(&t.ID, &t.EUBonusPerSlot); err != nil {
			return nil, fmt.Errorf("db: get unlocked upgrade tiers: %w", err)
		}
		tiers = append(tiers, t)
	}
	return tiers, rows.Err()
}
