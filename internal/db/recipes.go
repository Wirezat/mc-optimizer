package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/jackc/pgx/v5"
)

// GetRecipesForItem returns all recipes that output this item, highest priority first.
// Includes interface-compatible recipes: if machine A implements machine B,
// recipes of B are also returned as if they belong to A.
func (d *DB) GetRecipesForItem(ctx context.Context, itemModID, itemID string) ([]*model.RecipeRow, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT DISTINCT r.id, r.machine_mod_id, r.machine_id,
		                r.duration_ticks, r.total_eu, r.priority
		FROM recipes r
		JOIN recipe_item_outputs rio ON rio.recipe_id = r.id
		WHERE rio.item_mod_id = $1 AND rio.item_id = $2

		UNION

		SELECT DISTINCT r.id, mi.machine_mod_id, mi.machine_id,
		                r.duration_ticks, r.total_eu, r.priority
		FROM recipes r
		JOIN recipe_item_outputs rio ON rio.recipe_id = r.id
		JOIN machine_interfaces mi
		     ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
		WHERE rio.item_mod_id = $1 AND rio.item_id = $2

		ORDER BY priority DESC
	`, itemModID, itemID)
	if err != nil {
		return nil, fmt.Errorf("db: get recipes for item: %w", err)
	}
	defer rows.Close()

	var recipes []*model.RecipeRow
	for rows.Next() {
		r := &model.RecipeRow{}
		if err := rows.Scan(&r.ID, &r.MachineMod, &r.MachineID,
			&r.DurationTicks, &r.TotalEU, &r.Priority); err != nil {
			return nil, fmt.Errorf("db: get recipes for item: %w", err)
		}
		recipes = append(recipes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: get recipes for item: %w", err)
	}

	for _, r := range recipes {
		if err := d.loadRecipeIO(ctx, r); err != nil {
			return nil, fmt.Errorf("db: get recipes for item: %w", err)
		}
	}
	return recipes, nil
}

// GetRecipe returns the recipe with the given ID.
func (d *DB) GetRecipe(ctx context.Context, recipeID string) (*model.RecipeRow, error) {
	r := &model.RecipeRow{}
	err := d.Pool.QueryRow(ctx, `
		SELECT id, machine_mod_id, machine_id, duration_ticks, total_eu, priority
		FROM recipes WHERE id = $1
	`, recipeID).Scan(&r.ID, &r.MachineMod, &r.MachineID,
		&r.DurationTicks, &r.TotalEU, &r.Priority)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get recipe: %w", err)
	}
	if err := d.loadRecipeIO(ctx, r); err != nil {
		return nil, fmt.Errorf("db: get recipe: %w", err)
	}
	return r, nil
}

// GetMachineType returns the machine type for the given mod and machine IDs.
func (d *DB) GetMachineType(ctx context.Context, modID, machineID string) (*model.MachineType, error) {
	m := &model.MachineType{}
	err := d.Pool.QueryRow(ctx, `
		SELECT mod_id, machine_id, name,
		       COALESCE(base_eu_per_tick, 0),
		       COALESCE(max_eu_per_tick, 0),
		       COALESCE(max_slots, 0),
		       energy_type
		FROM machine_types
		WHERE mod_id = $1 AND machine_id = $2
	`, modID, machineID).Scan(
		&m.ModID, &m.MachineID, &m.Name,
		&m.BaseEUPerTick, &m.MaxEUPerTick, &m.MaxSlots,
		&m.EnergyType,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get machine type: %w", err)
	}
	return m, nil
}

// GetUpgradeTiers returns all upgrade tiers for a mod, cheapest (lowest EU bonus) first.
func (d *DB) GetUpgradeTiers(ctx context.Context, modID string) ([]*model.UpgradeTier, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, mod_id, name, eu_bonus_per_slot, item_ref
		FROM upgrade_tiers
		WHERE mod_id = $1
		ORDER BY eu_bonus_per_slot ASC
	`, modID)
	if err != nil {
		return nil, fmt.Errorf("db: get upgrade tiers: %w", err)
	}
	defer rows.Close()

	var tiers []*model.UpgradeTier
	for rows.Next() {
		t := &model.UpgradeTier{}
		var itemRef string
		if err := rows.Scan(&t.ID, &t.ModID, &t.Name, &t.EUBonusPerSlot, &itemRef); err != nil {
			return nil, fmt.Errorf("db: get upgrade tiers: %w", err)
		}
		t.ItemModID, t.ItemID = splitItemRef(itemRef)
		tiers = append(tiers, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: get upgrade tiers: %w", err)
	}
	return tiers, nil
}

// loadRecipeIO loads all item and fluid inputs/outputs for r into its slice fields.
func (d *DB) loadRecipeIO(ctx context.Context, r *model.RecipeRow) error {
	// Item Inputs
	rows, err := d.Pool.Query(ctx, `
		SELECT item_mod_id, item_id, tag_id,
		       amount_num, amount_den, probability_num, probability_den
		FROM recipe_item_inputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var in model.RecipeRowItemInput
		if err := rows.Scan(&in.ItemModID, &in.ItemID, &in.TagID,
			&in.AmountNum, &in.AmountDen, &in.ProbabilityNum, &in.ProbabilityDen); err != nil {
			return fmt.Errorf("db: load recipe io: %w", err)
		}
		r.ItemInputs = append(r.ItemInputs, in)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}

	// Item Outputs
	rows2, err := d.Pool.Query(ctx, `
		SELECT item_mod_id, item_id, amount_num, amount_den, probability_num, probability_den
		FROM recipe_item_outputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var out model.RecipeRowItemOutput
		if err := rows2.Scan(&out.ItemModID, &out.ItemID,
			&out.AmountNum, &out.AmountDen, &out.ProbabilityNum, &out.ProbabilityDen); err != nil {
			return fmt.Errorf("db: load recipe io: %w", err)
		}
		r.ItemOutputs = append(r.ItemOutputs, out)
	}
	if err := rows2.Err(); err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}

	// Fluid Inputs
	rows3, err := d.Pool.Query(ctx, `
		SELECT fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den
		FROM recipe_fluid_inputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		var fi model.RecipeRowFluidInput
		if err := rows3.Scan(&fi.FluidModID, &fi.FluidID,
			&fi.AmountMB, &fi.ProbabilityNum, &fi.ProbabilityDen); err != nil {
			return fmt.Errorf("db: load recipe io: %w", err)
		}
		r.FluidInputs = append(r.FluidInputs, fi)
	}
	if err := rows3.Err(); err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}

	// Fluid Outputs
	rows4, err := d.Pool.Query(ctx, `
		SELECT fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den
		FROM recipe_fluid_outputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	defer rows4.Close()
	for rows4.Next() {
		var fo model.RecipeRowFluidOutput
		if err := rows4.Scan(&fo.FluidModID, &fo.FluidID,
			&fo.AmountMB, &fo.ProbabilityNum, &fo.ProbabilityDen); err != nil {
			return fmt.Errorf("db: load recipe io: %w", err)
		}
		r.FluidOutputs = append(r.FluidOutputs, fo)
	}
	if err := rows4.Err(); err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	return nil
}

// splitItemRef splits "mod_id:item_id" into two strings; returns (ref, "") if no colon found.
func splitItemRef(ref string) (modID, itemID string) {
	for i, c := range ref {
		if c == ':' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, ""
}
