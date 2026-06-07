package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListMods returns all mods ordered by name.
func (d *DB) ListMods(ctx context.Context) ([]*model.Mod, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, name, energy_type
		FROM mods
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("db: list mods: %w", err)
	}
	defer rows.Close()

	var mods []*model.Mod
	for rows.Next() {
		m := &model.Mod{}
		if err := rows.Scan(&m.ModID, &m.Name, &m.EnergyType); err != nil {
			return nil, fmt.Errorf("db: scan mod: %w", err)
		}
		mods = append(mods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list mods: %w", err)
	}
	return mods, nil
}

// CreateMod inserts a new mod. Caller must be an admin (enforced at API layer).
func (d *DB) CreateMod(ctx context.Context, modID, name, energyType string) (*model.Mod, error) {
	m := &model.Mod{}
	err := d.Pool.QueryRow(ctx, `
		INSERT INTO mods (mod_id, name, energy_type)
		VALUES ($1, $2, $3)
		RETURNING mod_id, name, energy_type
	`, modID, name, energyType).Scan(&m.ModID, &m.Name, &m.EnergyType)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create mod: %w", err)
	}
	return m, nil
}

// UpdateMod patches name and/or energy_type for a mod. Caller must be an admin (enforced at API layer).
func (d *DB) UpdateMod(ctx context.Context, modID string, name, energyType *string) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE mods
		SET
			name        = COALESCE($2, name),
			energy_type = COALESCE($3, energy_type)
		WHERE mod_id = $1
	`, modID, name, energyType)
	if err != nil {
		return fmt.Errorf("db: update mod: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMachineType updates the display name and/or base EU/tick of a machine type.
// Caller must be an admin (enforced at API layer).
func (d *DB) UpdateMachineType(ctx context.Context, modID, machineID string, name *string, baseEUPerTick *int64) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_types
		SET
			name             = COALESCE($3, name),
			base_eu_per_tick = COALESCE($4, base_eu_per_tick)
		WHERE mod_id = $1 AND machine_id = $2
	`, modID, machineID, name, baseEUPerTick)
	if err != nil {
		return fmt.Errorf("db: update machine type: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateItem updates the display name of an item. Caller must be an admin (enforced at API layer).
func (d *DB) UpdateItem(ctx context.Context, modID, itemID, name string) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE items SET name = $3
		WHERE mod_id = $1 AND item_id = $2
	`, modID, itemID, name)
	if err != nil {
		return fmt.Errorf("db: update item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMod removes a mod (and cascades). Caller must be an admin (enforced at API layer).
func (d *DB) DeleteMod(ctx context.Context, modID string) error {
	tag, err := d.Pool.Exec(ctx, `
		DELETE FROM mods WHERE mod_id = $1
	`, modID)
	if err != nil {
		return fmt.Errorf("db: delete mod: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListItemsByMod returns all items for modID ordered by item_id.
func (d *DB) ListItemsByMod(ctx context.Context, modID string) ([]*model.Item, error) {
	var exists bool
	if err := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mods WHERE mod_id = $1)`, modID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("db: list items: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, item_id, name, max_stack
		FROM items
		WHERE mod_id = $1
		ORDER BY item_id
	`, modID)
	if err != nil {
		return nil, fmt.Errorf("db: list items: %w", err)
	}
	defer rows.Close()

	var items []*model.Item
	for rows.Next() {
		it := &model.Item{}
		if err := rows.Scan(&it.ModID, &it.ItemID, &it.Name, &it.MaxStack); err != nil {
			return nil, fmt.Errorf("db: scan item: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list items: %w", err)
	}
	return items, nil
}

// SearchItems returns up to 1000 items matching query q across all mods, ordered by name.
func (d *DB) SearchItems(ctx context.Context, q string) ([]*model.Item, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, item_id, name, max_stack
		FROM items
		WHERE $1 = '' OR name ILIKE '%' || $1 || '%' OR item_id ILIKE '%' || $1 || '%'
		ORDER BY name, mod_id
		LIMIT 1000
	`, q)
	if err != nil {
		return nil, fmt.Errorf("db: search items: %w", err)
	}
	defer rows.Close()

	var items []*model.Item
	for rows.Next() {
		it := &model.Item{}
		if err := rows.Scan(&it.ModID, &it.ItemID, &it.Name, &it.MaxStack); err != nil {
			return nil, fmt.Errorf("db: scan item: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: search items: %w", err)
	}
	return items, nil
}

// SearchFluids returns up to 1000 fluids matching query q across all mods, ordered by name.
func (d *DB) SearchFluids(ctx context.Context, q string) ([]*model.Fluid, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, fluid_id, name
		FROM fluids
		WHERE $1 = '' OR name ILIKE '%' || $1 || '%' OR fluid_id ILIKE '%' || $1 || '%'
		ORDER BY name, mod_id
		LIMIT 1000
	`, q)
	if err != nil {
		return nil, fmt.Errorf("db: search fluids: %w", err)
	}
	defer rows.Close()

	var fluids []*model.Fluid
	for rows.Next() {
		fl := &model.Fluid{}
		if err := rows.Scan(&fl.ModID, &fl.FluidID, &fl.Name); err != nil {
			return nil, fmt.Errorf("db: scan fluid: %w", err)
		}
		fluids = append(fluids, fl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: search fluids: %w", err)
	}
	return fluids, nil
}

// ListMachinesByMod returns all machine types for modID ordered by machine_id.
func (d *DB) ListMachinesByMod(ctx context.Context, modID string) ([]*model.MachineType, error) {
	var exists bool
	if err := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mods WHERE mod_id = $1)`, modID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("db: list machines: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, machine_id, name,
		       COALESCE(base_eu_per_tick, 0), COALESCE(max_eu_per_tick, 0),
		       COALESCE(max_slots, 0), COALESCE(energy_type, '')
		FROM machine_types
		WHERE mod_id = $1
		ORDER BY machine_id
	`, modID)
	if err != nil {
		return nil, fmt.Errorf("db: list machines: %w", err)
	}
	defer rows.Close()

	var machines []*model.MachineType
	for rows.Next() {
		mt := &model.MachineType{}
		if err := rows.Scan(
			&mt.ModID, &mt.MachineID, &mt.Name,
			&mt.BaseEUPerTick, &mt.MaxEUPerTick, &mt.MaxSlots,
			&mt.EnergyType,
		); err != nil {
			return nil, fmt.Errorf("db: scan machine: %w", err)
		}
		machines = append(machines, mt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list machines: %w", err)
	}
	return machines, nil
}

// DeleteRecipe removes a recipe (and its IO rows via CASCADE). Caller must be an admin (enforced at API layer).
func (d *DB) DeleteRecipe(ctx context.Context, recipeID string) error {
	tag, err := d.Pool.Exec(ctx, `
		DELETE FROM recipes WHERE id::text = $1
	`, recipeID)
	if err != nil {
		return fmt.Errorf("db: delete recipe: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateRecipeName sets or clears the display name of a recipe. Caller must be an admin (enforced at API layer).
func (d *DB) UpdateRecipeName(ctx context.Context, recipeID string, name *string) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE recipes SET name = $2 WHERE id::text = $1
	`, recipeID, name)
	if err != nil {
		return fmt.Errorf("db: update recipe name: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRecipesByMod returns recipes for modID, optionally filtered by machineID, with IO hydrated.
// Includes interface-compatible recipes: recipes belonging to a base machine that modID implements.
func (d *DB) ListRecipesByMod(ctx context.Context, modID, machineID string) ([]*model.Recipe, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id::text, machine_mod_id, machine_id, name, duration_ticks, eu_per_tick, total_eu, priority
		FROM recipes
		WHERE machine_mod_id = $1
		  AND ($2 = '' OR machine_id = $2)

		UNION

		SELECT r.id::text, mi.machine_mod_id, mi.machine_id,
		       r.name, r.duration_ticks, r.eu_per_tick, r.total_eu, r.priority
		FROM recipes r
		JOIN machine_interfaces mi
		     ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
		WHERE mi.machine_mod_id = $1
		  AND ($2 = '' OR mi.machine_id = $2)

		ORDER BY machine_id, priority DESC
	`, modID, machineID)
	if err != nil {
		return nil, fmt.Errorf("db: list recipes: %w", err)
	}
	defer rows.Close()

	var recipes []*model.Recipe
	for rows.Next() {
		rec := &model.Recipe{}
		if err := rows.Scan(
			&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.Name,
			&rec.DurationTicks, &rec.EUPerTick, &rec.TotalEU, &rec.Priority,
		); err != nil {
			return nil, fmt.Errorf("db: scan recipe: %w", err)
		}
		recipes = append(recipes, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list recipes: %w", err)
	}

	for _, rec := range recipes {
		if err := d.hydrateRecipeIO(ctx, rec); err != nil {
			return nil, err
		}
	}
	return recipes, nil
}

// CreateRecipe inserts a recipe with all IO slots in a transaction.
// Caller must be an admin (enforced at API layer).
func (d *DB) CreateRecipe(ctx context.Context, modID string, req *model.CreateRecipeRequest) (*model.Recipe, error) {
	var exists bool
	if err := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mods WHERE mod_id = $1)`, modID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("db: create recipe: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: create recipe: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Insert recipe.
	rec := &model.Recipe{}
	if err := tx.QueryRow(ctx, `
		INSERT INTO recipes (id, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu, priority)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
		RETURNING id::text, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu, priority
	`, modID, req.MachineID, req.DurationTicks, req.EUPerTick, int64(req.DurationTicks)*req.EUPerTick, req.Priority,
	).Scan(&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.DurationTicks, &rec.EUPerTick, &rec.TotalEU, &rec.Priority); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create recipe: %w", err)
	}

	recipeID := rec.ID

	// 2. Insert item inputs.
	for _, in := range req.ItemInputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_inputs
				(id, recipe_id, item_mod_id, item_id, tag_id, amount_num, amount_den, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		`, recipeID, in.ItemModID, in.ItemID, in.TagID,
			in.AmountNum, in.AmountDen, in.ProbabilityNum, in.ProbabilityDen,
		); err != nil {
			return nil, fmt.Errorf("db: create recipe: %w", err)
		}
	}

	// 3. Insert item outputs.
	for _, out := range req.ItemOutputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_outputs
				(id, recipe_id, item_mod_id, item_id, amount_num, amount_den, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		`, recipeID, out.ItemModID, out.ItemID,
			out.AmountNum, out.AmountDen, out.ProbabilityNum, out.ProbabilityDen,
		); err != nil {
			return nil, fmt.Errorf("db: create recipe: %w", err)
		}
	}

	// 4. Insert fluid inputs.
	for _, in := range req.FluidInputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_inputs
				(id, recipe_id, fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
		`, recipeID, in.FluidModID, in.FluidID,
			in.AmountMB, in.ProbabilityNum, in.ProbabilityDen,
		); err != nil {
			return nil, fmt.Errorf("db: create recipe: %w", err)
		}
	}

	// 5. Insert fluid outputs.
	for _, out := range req.FluidOutputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_outputs
				(id, recipe_id, fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
		`, recipeID, out.FluidModID, out.FluidID,
			out.AmountMB, out.ProbabilityNum, out.ProbabilityDen,
		); err != nil {
			return nil, fmt.Errorf("db: create recipe: %w", err)
		}
	}

	// 6. Commit transaction.
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("db: create recipe: %w", err)
	}

	if err := d.hydrateRecipeIO(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func (d *DB) hydrateRecipeIO(ctx context.Context, rec *model.Recipe) error {
	// Item inputs.
	rows, err := d.Pool.Query(ctx, `
		SELECT rii.id::text, rii.recipe_id::text, rii.item_mod_id, rii.item_id,
		       i.name,
		       rii.tag_id::text, t.name,
		       rii.amount_num, rii.amount_den, rii.probability_num, rii.probability_den
		FROM recipe_item_inputs rii
		LEFT JOIN tags  t ON t.id = rii.tag_id
		LEFT JOIN items i ON i.mod_id = rii.item_mod_id AND i.item_id = rii.item_id
		WHERE rii.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		in := model.RecipeItemInput{}
		if err := rows.Scan(&in.ID, &in.RecipeID, &in.ItemModID, &in.ItemID, &in.ItemName,
			&in.TagID, &in.TagName,
			&in.AmountNum, &in.AmountDen, &in.ProbabilityNum, &in.ProbabilityDen); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		rec.ItemInputs = append(rec.ItemInputs, in)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}

	// Item outputs.
	rows2, err := d.Pool.Query(ctx, `
		SELECT rio.id::text, rio.recipe_id::text, rio.item_mod_id, rio.item_id,
		       i.name,
		       rio.amount_num, rio.amount_den, rio.probability_num, rio.probability_den
		FROM recipe_item_outputs rio
		LEFT JOIN items i ON i.mod_id = rio.item_mod_id AND i.item_id = rio.item_id
		WHERE rio.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		out := model.RecipeItemOutput{}
		if err := rows2.Scan(&out.ID, &out.RecipeID, &out.ItemModID, &out.ItemID, &out.ItemName,
			&out.AmountNum, &out.AmountDen, &out.ProbabilityNum, &out.ProbabilityDen); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		rec.ItemOutputs = append(rec.ItemOutputs, out)
	}
	if err := rows2.Err(); err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}

	// Fluid inputs.
	rows3, err := d.Pool.Query(ctx, `
		SELECT rfi.id::text, rfi.recipe_id::text, rfi.fluid_mod_id, rfi.fluid_id,
		       f.name,
		       rfi.amount_mb, rfi.probability_num, rfi.probability_den
		FROM recipe_fluid_inputs rfi
		LEFT JOIN fluids f ON f.mod_id = rfi.fluid_mod_id AND f.fluid_id = rfi.fluid_id
		WHERE rfi.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		in := model.RecipeFluidInput{}
		if err := rows3.Scan(&in.ID, &in.RecipeID, &in.FluidModID, &in.FluidID, &in.FluidName,
			&in.AmountMB, &in.ProbabilityNum, &in.ProbabilityDen); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		rec.FluidInputs = append(rec.FluidInputs, in)
	}
	if err := rows3.Err(); err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}

	// Fluid outputs.
	rows4, err := d.Pool.Query(ctx, `
		SELECT rfo.id::text, rfo.recipe_id::text, rfo.fluid_mod_id, rfo.fluid_id,
		       f.name,
		       rfo.amount_mb, rfo.probability_num, rfo.probability_den
		FROM recipe_fluid_outputs rfo
		LEFT JOIN fluids f ON f.mod_id = rfo.fluid_mod_id AND f.fluid_id = rfo.fluid_id
		WHERE rfo.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows4.Close()
	for rows4.Next() {
		out := model.RecipeFluidOutput{}
		if err := rows4.Scan(&out.ID, &out.RecipeID, &out.FluidModID, &out.FluidID, &out.FluidName,
			&out.AmountMB, &out.ProbabilityNum, &out.ProbabilityDen); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		rec.FluidOutputs = append(rec.FluidOutputs, out)
	}
	if err := rows4.Err(); err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	return nil
}

// MachineInterface represents a single "A implements B" relationship.
type MachineInterface struct {
	MachineModID  string `json:"machine_mod_id"`
	MachineID     string `json:"machine_id"`
	BaseModID     string `json:"base_mod_id"`
	BaseMachineID string `json:"base_machine_id"`
}

// ListMachineInterfaces returns all interfaces declared for (modID, machineID).
func (d *DB) ListMachineInterfaces(ctx context.Context, modID, machineID string) ([]MachineInterface, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT machine_mod_id, machine_id, base_mod_id, base_machine_id
		FROM machine_interfaces
		WHERE machine_mod_id = $1 AND machine_id = $2
		ORDER BY base_mod_id, base_machine_id
	`, modID, machineID)
	if err != nil {
		return nil, fmt.Errorf("db: list machine interfaces: %w", err)
	}
	defer rows.Close()

	var result []MachineInterface
	for rows.Next() {
		var mi MachineInterface
		if err := rows.Scan(&mi.MachineModID, &mi.MachineID, &mi.BaseModID, &mi.BaseMachineID); err != nil {
			return nil, fmt.Errorf("db: scan machine interface: %w", err)
		}
		result = append(result, mi)
	}
	return result, rows.Err()
}

// AddMachineInterface adds an "A implements B" relationship.
// Caller must be an admin (enforced at API layer).
func (d *DB) AddMachineInterface(ctx context.Context, modID, machineID, baseModID, baseMachineID string) error {
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO machine_interfaces (machine_mod_id, machine_id, base_mod_id, base_machine_id)
		SELECT $1, $2, $3, $4
		WHERE EXISTS (SELECT 1 FROM machine_types WHERE mod_id = $1 AND machine_id = $2)
		  AND EXISTS (SELECT 1 FROM machine_types WHERE mod_id = $3 AND machine_id = $4)
	`, modID, machineID, baseModID, baseMachineID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("db: add machine interface: %w", err)
	}
	return nil
}

// DeleteMachineInterface removes an "A implements B" relationship.
// Caller must be an admin (enforced at API layer).
func (d *DB) DeleteMachineInterface(ctx context.Context, modID, machineID, baseModID, baseMachineID string) error {
	tag, err := d.Pool.Exec(ctx, `
		DELETE FROM machine_interfaces
		WHERE machine_mod_id = $1 AND machine_id = $2
		  AND base_mod_id = $3 AND base_machine_id = $4
	`, modID, machineID, baseModID, baseMachineID)
	if err != nil {
		return fmt.Errorf("db: delete machine interface: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
