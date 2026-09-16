package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/jackc/pgx/v5"
)

// GetRecipesForItem returns every (recipe, machine) candidate that outputs this
// item: the recipe's own machine, plus each machine that implements it via
// machine_interfaces. One recipe therefore yields several rows, distinguished
// by machine - solver.RecipeOptionKey is what tells them apart. Within a recipe
// the machine that owns it sorts first, so a caller taking the first row still
// gets the base machine.
func (d *DB) GetRecipesForItem(ctx context.Context, itemModID, itemID string) ([]*solver.RecipeRow, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, machine_mod_id, machine_id, duration_ticks, mod_data
		FROM (
			SELECT DISTINCT r.id, r.machine_mod_id, r.machine_id,
			                r.duration_ticks, r.mod_data, 0 AS is_direct
			FROM recipes r
			JOIN recipe_item_outputs rio ON rio.recipe_id = r.id
			WHERE rio.item_mod_id = $1 AND rio.item_id = $2
			  AND r.machine_id NOT IN ('crafting_table', 'unpacker')

			UNION

			SELECT DISTINCT r.id, mi.machine_mod_id, mi.machine_id,
			                r.duration_ticks, r.mod_data, 1 AS is_direct
			FROM recipes r
			JOIN recipe_item_outputs rio ON rio.recipe_id = r.id
			JOIN machine_interfaces mi
			     ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
			WHERE rio.item_mod_id = $1 AND rio.item_id = $2
			  AND r.machine_id NOT IN ('crafting_table', 'unpacker')
			  AND (mi.machine_mod_id, mi.machine_id) IS DISTINCT FROM (r.machine_mod_id, r.machine_id)
		) sub
		ORDER BY id, is_direct, machine_id
	`, itemModID, itemID)
	if err != nil {
		return nil, fmt.Errorf("db: get recipes for item: %w", err)
	}
	return d.scanRecipeRows(ctx, rows, "get recipes for item")
}

// GetRecipesForFluid returns every (recipe, machine) candidate that outputs this
// fluid: the recipe's own machine, plus each machine that implements it via
// machine_interfaces. One recipe therefore yields several rows, distinguished
// by machine - solver.RecipeOptionKey is what tells them apart. Within a recipe
// the machine that owns it sorts first, so a caller taking the first row still
// gets the base machine.
func (d *DB) GetRecipesForFluid(ctx context.Context, fluidModID, fluidID string) ([]*solver.RecipeRow, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, machine_mod_id, machine_id, duration_ticks, mod_data
		FROM (
			SELECT DISTINCT r.id, r.machine_mod_id, r.machine_id,
			                r.duration_ticks, r.mod_data, 0 AS is_direct
			FROM recipes r
			JOIN recipe_fluid_outputs rfo ON rfo.recipe_id = r.id
			WHERE rfo.fluid_mod_id = $1 AND rfo.fluid_id = $2

			UNION

			SELECT DISTINCT r.id, mi.machine_mod_id, mi.machine_id,
			                r.duration_ticks, r.mod_data, 1 AS is_direct
			FROM recipes r
			JOIN recipe_fluid_outputs rfo ON rfo.recipe_id = r.id
			JOIN machine_interfaces mi
			     ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
			WHERE rfo.fluid_mod_id = $1 AND rfo.fluid_id = $2
			  AND (mi.machine_mod_id, mi.machine_id) IS DISTINCT FROM (r.machine_mod_id, r.machine_id)
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
		var modData []byte
		if err := rows.Scan(&r.ID, &r.MachineMod, &r.MachineID, &r.DurationTicks, &modData); err != nil {
			return nil, fmt.Errorf("db: %s: %w", label, err)
		}
		r.ModData = json.RawMessage(modData)
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
	var modData []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT id, machine_mod_id, machine_id, duration_ticks, mod_data
		FROM recipes WHERE id = $1
	`, recipeID).Scan(&r.ID, &r.MachineMod, &r.MachineID, &r.DurationTicks, &modData)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get recipe: %w", err)
	}
	r.ModData = json.RawMessage(modData)
	if err := d.loadRecipeIO(ctx, r); err != nil {
		return nil, fmt.Errorf("db: get recipe: %w", err)
	}
	return r, nil
}

// GetMachinesForRecipe returns every machine that can run this recipe, the one
// that owns it first, then each machine implementing it via machine_interfaces.
func (d *DB) GetMachinesForRecipe(ctx context.Context, recipeID string) ([]solver.MachineRef, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT machine_mod_id, machine_id FROM (
			SELECT r.machine_mod_id, r.machine_id, 0 AS is_direct
			FROM recipes r
			WHERE r.id = $1

			UNION

			SELECT mi.machine_mod_id, mi.machine_id, 1 AS is_direct
			FROM recipes r
			JOIN machine_interfaces mi
			     ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
			WHERE r.id = $1
			  AND (mi.machine_mod_id, mi.machine_id) IS DISTINCT FROM (r.machine_mod_id, r.machine_id)
		) sub
		ORDER BY is_direct, machine_mod_id, machine_id
	`, recipeID)
	if err != nil {
		return nil, fmt.Errorf("db: get machines for recipe: %w", err)
	}
	defer rows.Close()
	var out []solver.MachineRef
	for rows.Next() {
		var m solver.MachineRef
		if err := rows.Scan(&m.ModID, &m.MachineID); err != nil {
			return nil, fmt.Errorf("db: get machines for recipe: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: get machines for recipe: %w", err)
	}
	return out, nil
}

// GetMachineType returns the machine spec fields needed by the solver.
func (d *DB) GetMachineType(ctx context.Context, modID, machineID string) (*solver.MachineSpec, error) {
	m := &solver.MachineSpec{}
	var modData []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT mod_id, machine_id, name, COALESCE(ecosystem, ''), mod_data
		FROM machine_types
		WHERE mod_id = $1 AND machine_id = $2
	`, modID, machineID).Scan(&m.ModID, &m.MachineID, &m.Name, &m.Ecosystem, &modData)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("db: get machine type: %w", err)
	}
	m.ModData = json.RawMessage(modData)
	return m, nil
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
		ORDER BY rii.sort_index
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
		ORDER BY sort_index
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
		ORDER BY sort_index
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
		ORDER BY sort_index
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
