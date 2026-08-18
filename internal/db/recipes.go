package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/jackc/pgx/v5"
)

// maxAchievableEUExpr is the highest EU/t a machine (mt) could ever supply a
// recipe, i.e. its tier cap plus the best possible upgrade bonus. Mirrors MI's
// own CrafterComponent.banRecipe default (recipe.eu > getMaxRecipeEu()) — a
// recipe that no amount of upgrading could ever satisfy must never be offered
// as an option, not just run slowly.
//
// Upgrade capacity is per UPGRADE ITEM (MI's UpgradeComponent holds a single
// ItemStack; bonus = itemStack.getCount() * extraMaxEu), not per machine —
// machine_types.max_slots has no bearing on how many upgrade items fit. The
// item's own max_stack (items table) is the real cap, so the best-case bonus
// is MAX(tier.eu_bonus_per_slot * item.max_stack) across all tiers for this
// mod, not machine.max_slots * best single-tier bonus.
const maxAchievableEUExpr = `
	COALESCE(NULLIF(mt.max_eu_per_tick, 0), mt.base_eu_per_tick, r.eu_per_tick)
	+ CASE WHEN mt.upgradable THEN
		COALESCE((
			SELECT MAX(ut.eu_bonus_per_slot * COALESCE(i.max_stack, 64))
			FROM upgrade_tiers ut
			LEFT JOIN items i
			     ON i.mod_id = split_part(ut.item_ref, ':', 1) AND i.item_id = split_part(ut.item_ref, ':', 2)
			WHERE ut.mod_id = mt.mod_id
		), 0)
	ELSE 0 END`

// GetRecipesForItem returns all recipes that output this item, one row per
// recipe. Includes interface-compatible recipes (if machine A implements
// machine B, recipes of B are also considered A's, capped by A's max EU/t),
// folding a recipe that qualifies for both its base machine and an
// implementer to the base machine's row.
func (d *DB) GetRecipesForItem(ctx context.Context, itemModID, itemID string) ([]*solver.RecipeRow, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT DISTINCT ON (id) id, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu
		FROM (
			SELECT DISTINCT r.id, r.machine_mod_id, r.machine_id,
			                r.duration_ticks, r.eu_per_tick, r.total_eu, 0 AS is_direct
			FROM recipes r
			JOIN machine_types mt
			     ON mt.mod_id = r.machine_mod_id AND mt.machine_id = r.machine_id
			JOIN recipe_item_outputs rio ON rio.recipe_id = r.id
			WHERE rio.item_mod_id = $1 AND rio.item_id = $2
			  AND r.machine_id NOT IN ('crafting_table', 'unpacker')
			  AND (mt.energy_type <> 'eu' OR r.eu_per_tick <= `+maxAchievableEUExpr+`)
			  AND (mt.fixed_recipe_eu_cap IS NULL OR mt.fixed_recipe_eu_cap = 0 OR r.eu_per_tick <= mt.fixed_recipe_eu_cap)

			UNION

			SELECT DISTINCT r.id, mi.machine_mod_id, mi.machine_id,
			                r.duration_ticks, r.eu_per_tick, r.total_eu, 1 AS is_direct
			FROM recipes r
			JOIN recipe_item_outputs rio ON rio.recipe_id = r.id
			JOIN machine_interfaces mi
			     ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
			JOIN machine_types mt
			     ON mt.mod_id = mi.machine_mod_id AND mt.machine_id = mi.machine_id
			WHERE rio.item_mod_id = $1 AND rio.item_id = $2
			  AND r.machine_id NOT IN ('crafting_table', 'unpacker')
			  AND (mt.energy_type <> 'eu' OR r.eu_per_tick <= `+maxAchievableEUExpr+`)
			  AND (mt.fixed_recipe_eu_cap IS NULL OR mt.fixed_recipe_eu_cap = 0 OR r.eu_per_tick <= mt.fixed_recipe_eu_cap)
		) sub
		ORDER BY id, is_direct, machine_id
	`, itemModID, itemID)
	if err != nil {
		return nil, fmt.Errorf("db: get recipes for item: %w", err)
	}
	return d.scanRecipeRows(ctx, rows, "get recipes for item")
}

// GetRecipesForFluid returns all recipes that output this fluid, one row per
// recipe. Same interface/folding behavior as GetRecipesForItem.
func (d *DB) GetRecipesForFluid(ctx context.Context, fluidModID, fluidID string) ([]*solver.RecipeRow, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT DISTINCT ON (id) id, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu
		FROM (
			SELECT DISTINCT r.id, r.machine_mod_id, r.machine_id,
			                r.duration_ticks, r.eu_per_tick, r.total_eu, 0 AS is_direct
			FROM recipes r
			JOIN machine_types mt
			     ON mt.mod_id = r.machine_mod_id AND mt.machine_id = r.machine_id
			JOIN recipe_fluid_outputs rfo ON rfo.recipe_id = r.id
			WHERE rfo.fluid_mod_id = $1 AND rfo.fluid_id = $2
			  AND (mt.energy_type <> 'eu' OR r.eu_per_tick <= `+maxAchievableEUExpr+`)
			  AND (mt.fixed_recipe_eu_cap IS NULL OR mt.fixed_recipe_eu_cap = 0 OR r.eu_per_tick <= mt.fixed_recipe_eu_cap)

			UNION

			SELECT DISTINCT r.id, mi.machine_mod_id, mi.machine_id,
			                r.duration_ticks, r.eu_per_tick, r.total_eu, 1 AS is_direct
			FROM recipes r
			JOIN recipe_fluid_outputs rfo ON rfo.recipe_id = r.id
			JOIN machine_interfaces mi
			     ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
			JOIN machine_types mt
			     ON mt.mod_id = mi.machine_mod_id AND mt.machine_id = mi.machine_id
			WHERE rfo.fluid_mod_id = $1 AND rfo.fluid_id = $2
			  AND (mt.energy_type <> 'eu' OR r.eu_per_tick <= `+maxAchievableEUExpr+`)
			  AND (mt.fixed_recipe_eu_cap IS NULL OR mt.fixed_recipe_eu_cap = 0 OR r.eu_per_tick <= mt.fixed_recipe_eu_cap)
		) sub
		ORDER BY id, is_direct, machine_id
	`, fluidModID, fluidID)
	if err != nil {
		return nil, fmt.Errorf("db: get recipes for fluid: %w", err)
	}
	return d.scanRecipeRows(ctx, rows, "get recipes for fluid")
}

// scanRecipeRows scans a query result into RecipeRows and loads their I/O.
func (d *DB) scanRecipeRows(ctx context.Context, rows pgx.Rows, label string) ([]*solver.RecipeRow, error) {
	defer rows.Close()
	var recipes []*solver.RecipeRow
	for rows.Next() {
		r := &solver.RecipeRow{}
		if err := rows.Scan(&r.ID, &r.MachineMod, &r.MachineID,
			&r.DurationTicks, &r.EUPerTick, &r.TotalEU); err != nil {
			return nil, fmt.Errorf("db: %s: %w", label, err)
		}
		recipes = append(recipes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: %s: %w", label, err)
	}
	for _, r := range recipes {
		if err := d.loadRecipeIO(ctx, r); err != nil {
			return nil, fmt.Errorf("db: %s: %w", label, err)
		}
	}
	return recipes, nil
}

// GetRecipe returns the recipe with the given ID.
func (d *DB) GetRecipe(ctx context.Context, recipeID string) (*solver.RecipeRow, error) {
	r := &solver.RecipeRow{}
	err := d.Pool.QueryRow(ctx, `
		SELECT id, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu
		FROM recipes WHERE id = $1
	`, recipeID).Scan(&r.ID, &r.MachineMod, &r.MachineID,
		&r.DurationTicks, &r.EUPerTick, &r.TotalEU)
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

// GetMachineType returns the machine spec fields needed by the solver.
func (d *DB) GetMachineType(ctx context.Context, modID, machineID string) (*solver.MachineSpec, error) {
	m := &solver.MachineSpec{}
	err := d.Pool.QueryRow(ctx, `
		SELECT mod_id, machine_id,
		       COALESCE(energy_type, ''),
		       COALESCE(base_eu_per_tick, 0),
		       COALESCE(max_eu_per_tick, 0),
		       COALESCE(max_slots, 0),
		       COALESCE(upgradable, false),
		       COALESCE(fixed_recipe_eu_cap, 0)
		FROM machine_types
		WHERE mod_id = $1 AND machine_id = $2
	`, modID, machineID).Scan(
		&m.ModID, &m.MachineID,
		&m.EnergyType, &m.BaseEUPerTick, &m.MaxEUPerTick, &m.MaxSlots,
		&m.Upgradable, &m.FixedRecipeEUCap,
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
// The upgrade count cap (MaxStackSize) is read from the referenced item's own max_stack
// (items table) — not duplicated on upgrade_tiers — since MI's UpgradeComponent holds a
// single ItemStack of the upgrade item, so the item's real stack size IS the cap.
func (d *DB) GetUpgradeTiers(ctx context.Context, modID string) ([]*solver.UpgradeTierSpec, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT ut.id, ut.eu_bonus_per_slot, COALESCE(i.max_stack, 64)
		FROM upgrade_tiers ut
		LEFT JOIN items i
		     ON i.mod_id = split_part(ut.item_ref, ':', 1) AND i.item_id = split_part(ut.item_ref, ':', 2)
		WHERE ut.mod_id = $1
		ORDER BY ut.eu_bonus_per_slot ASC
	`, modID)
	if err != nil {
		return nil, fmt.Errorf("db: get upgrade tiers: %w", err)
	}
	defer rows.Close()

	var tiers []*solver.UpgradeTierSpec
	for rows.Next() {
		t := &solver.UpgradeTierSpec{}
		if err := rows.Scan(&t.ID, &t.EUBonusPerSlot, &t.MaxStackSize); err != nil {
			return nil, fmt.Errorf("db: get upgrade tiers: %w", err)
		}
		tiers = append(tiers, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: get upgrade tiers: %w", err)
	}
	return tiers, nil
}

// GetTagMembers returns all concrete items that satisfy the given tag, ordered by mod_id, item_id.
func (d *DB) GetTagMembers(ctx context.Context, tagName string) ([]solver.ItemRef, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT tm.item_mod_id, tm.item_id
		FROM tag_members tm
		JOIN tags t ON t.id = tm.tag_id
		WHERE t.name = $1
		ORDER BY tm.item_mod_id, tm.item_id
	`, tagName)
	if err != nil {
		return nil, fmt.Errorf("db: get tag members: %w", err)
	}
	defer rows.Close()
	var refs []solver.ItemRef
	for rows.Next() {
		var r solver.ItemRef
		if err := rows.Scan(&r.ModID, &r.ItemID); err != nil {
			return nil, fmt.Errorf("db: get tag members: %w", err)
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// loadRecipeIO loads all item and fluid inputs/outputs for r into its slice fields.
func (d *DB) loadRecipeIO(ctx context.Context, r *solver.RecipeRow) error {
	// Item Inputs — LEFT JOIN tags; COALESCE nullables to '' to avoid pgx NULL scan issues.
	rows, err := d.Pool.Query(ctx, `
		SELECT COALESCE(rii.item_mod_id, ''), COALESCE(rii.item_id, ''),
		       COALESCE(rii.tag_id::text, ''), COALESCE(t.name, ''),
		       rii.amount_num, rii.amount_den, rii.probability_num, rii.probability_den,
		       rii.non_consuming
		FROM recipe_item_inputs rii
		LEFT JOIN tags t ON t.id = rii.tag_id
		WHERE rii.recipe_id = $1
	`, r.ID)
	if err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var in solver.RecipeRowItemIO
		var modID, itemID, tagID, tagName string
		if err := rows.Scan(&modID, &itemID, &tagID, &tagName,
			&in.AmountNum, &in.AmountDen, &in.ProbabilityNum, &in.ProbabilityDen,
			&in.NonConsuming); err != nil {
			return fmt.Errorf("db: load recipe io: %w", err)
		}
		in.ItemModID = strPtrOr(modID)
		in.ItemID = strPtrOr(itemID)
		in.TagID = strPtrOr(tagID)
		in.TagName = strPtrOr(tagName)
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
		var out solver.RecipeRowItemIO
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
		SELECT COALESCE(fluid_mod_id, ''), COALESCE(fluid_id, ''),
		       amount_mb, probability_num, probability_den
		FROM recipe_fluid_inputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		var fi solver.RecipeRowFluidIO
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
		SELECT COALESCE(fluid_mod_id, ''), COALESCE(fluid_id, ''),
		       amount_mb, probability_num, probability_den
		FROM recipe_fluid_outputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return fmt.Errorf("db: load recipe io: %w", err)
	}
	defer rows4.Close()
	for rows4.Next() {
		var fo solver.RecipeRowFluidIO
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
