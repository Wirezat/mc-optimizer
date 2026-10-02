package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/jackc/pgx/v5"
)

// GetRecipesFor returns every (recipe, machine) candidate that outputs ref: the recipe's own
// machine, plus each machine that implements it via machine_interfaces.
func (d *DB) GetRecipesFor(ctx context.Context, ref resource.Ref) ([]*solver.RecipeRow, error) {
	switch ref.Kind.Or() {
	case resource.KindItem:
		return d.getRecipesForItem(ctx, ref.ModID, ref.ID)
	case resource.KindFluid:
		return d.getRecipesForFluid(ctx, ref.ModID, ref.ID)
	}
	return nil, fmt.Errorf("db: no recipe lookup for kind %q", ref.Kind)
}

func (d *DB) getRecipesForItem(ctx context.Context, itemModID, itemID string) ([]*solver.RecipeRow, error) {
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

func (d *DB) getRecipesForFluid(ctx context.Context, fluidModID, fluidID string) ([]*solver.RecipeRow, error) {
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

// GetMachinesForRecipe returns every machine that can run this recipe, the one that owns it
// first, then each machine implementing it via machine_interfaces.
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

// GetTagMembers returns every item, or for a fluid tag every fluid, that satisfies tag,
// ordered by mod and id.
func (d *DB) GetTagMembers(ctx context.Context, tag resource.Ref) ([]resource.Ref, error) {
	query := `
		SELECT DISTINCT tm.item_mod_id, tm.item_id
		FROM tag_members tm
		JOIN tags t ON t.id = tm.tag_id
		WHERE t.kind = 'item' AND t.name = $1
		ORDER BY 1, 2`
	if tag.Kind.Or() == resource.KindFluid {
		query = `
		SELECT DISTINCT tm.fluid_mod_id, tm.fluid_id
		FROM tag_fluid_members tm
		JOIN tags t ON t.id = tm.tag_id
		WHERE t.kind = 'fluid' AND t.name = $1
		ORDER BY 1, 2`
	}
	rows, err := d.Pool.Query(ctx, query, tag.TagRef)
	if err != nil {
		return nil, fmt.Errorf("db: get tag members: %w", err)
	}
	defer rows.Close()
	var refs []resource.Ref
	for rows.Next() {
		r := resource.Ref{Kind: tag.Kind}
		if err := rows.Scan(&r.ModID, &r.ID); err != nil {
			return nil, fmt.Errorf("db: get tag members: %w", err)
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// loadRecipeIO loads all item and fluid inputs/outputs for r into its slice fields.
func (d *DB) loadRecipeIO(ctx context.Context, r *solver.RecipeRow) error {
	queries := []struct {
		kind   resource.Kind
		output bool
		sql    string
	}{
		{resource.KindItem, false, `
			SELECT COALESCE(io.item_mod_id, ''), COALESCE(io.item_id, ''), COALESCE(t.name, ''),
			       io.amount_num, io.amount_den, io.probability_num, io.probability_den, io.non_consuming
			FROM recipe_item_inputs io LEFT JOIN tags t ON t.id = io.tag_id
			WHERE io.recipe_id = $1 ORDER BY io.sort_index`},
		{resource.KindFluid, false, `
			SELECT COALESCE(io.fluid_mod_id, ''), COALESCE(io.fluid_id, ''), COALESCE(t.name, ''),
			       io.amount_mb, 1, io.probability_num, io.probability_den, io.non_consuming
			FROM recipe_fluid_inputs io LEFT JOIN tags t ON t.id = io.tag_id
			WHERE io.recipe_id = $1 ORDER BY io.sort_index`},
		{resource.KindItem, true, `
			SELECT io.item_mod_id, io.item_id, '',
			       io.amount_num, io.amount_den, io.probability_num, io.probability_den, false
			FROM recipe_item_outputs io
			WHERE io.recipe_id = $1 ORDER BY io.sort_index`},
		{resource.KindFluid, true, `
			SELECT COALESCE(io.fluid_mod_id, ''), COALESCE(io.fluid_id, ''), COALESCE(t.name, ''),
			       io.amount_mb, 1, io.probability_num, io.probability_den, false
			FROM recipe_fluid_outputs io LEFT JOIN tags t ON t.id = io.tag_id
			WHERE io.recipe_id = $1 ORDER BY io.sort_index`},
	}
	for _, q := range queries {
		rows, err := d.Pool.Query(ctx, q.sql, r.ID)
		if err != nil {
			return fmt.Errorf("db: load recipe io: %w", err)
		}
		for rows.Next() {
			var ref resource.Ref
			var amountNum, amountDen, probNum, probDen int64
			var tool bool
			if err := rows.Scan(&ref.ModID, &ref.ID, &ref.TagRef, &amountNum, &amountDen, &probNum, &probDen, &tool); err != nil {
				rows.Close()
				return fmt.Errorf("db: load recipe io: %w", err)
			}
			ref.Kind = q.kind
			io := resource.IO{Ref: ref, Amount: resource.NewRational(amountNum, amountDen), Prob: resource.NewRational(probNum, probDen), Consumed: !tool}
			if q.output {
				r.Outputs = append(r.Outputs, io)
			} else {
				r.Inputs = append(r.Inputs, io)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("db: load recipe io: %w", err)
		}
	}
	return nil
}
