package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListValidRecipeTypes returns all registered valid recipe type patterns.
func (d *DB) ListValidRecipeTypes(ctx context.Context) ([]*model.ValidRecipeType, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, pattern, is_regex, target_mod_id, target_machine_id
		FROM valid_recipe_types
		ORDER BY pattern
	`)
	if err != nil {
		return nil, fmt.Errorf("db: list valid recipe types: %w", err)
	}
	defer rows.Close()

	var result []*model.ValidRecipeType
	for rows.Next() {
		v := &model.ValidRecipeType{}
		if err := rows.Scan(&v.ID, &v.Pattern, &v.IsRegex, &v.TargetModID, &v.TargetMachineID); err != nil {
			return nil, fmt.Errorf("db: list valid recipe types: scan: %w", err)
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// CreateValidRecipeType inserts a new valid recipe type pattern.
func (d *DB) CreateValidRecipeType(ctx context.Context, pattern string, isRegex bool, targetModID, targetMachineID *string) (*model.ValidRecipeType, error) {
	v := &model.ValidRecipeType{}
	err := d.Pool.QueryRow(ctx, `
		INSERT INTO valid_recipe_types (id, pattern, is_regex, target_mod_id, target_machine_id)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		RETURNING id, pattern, is_regex, target_mod_id, target_machine_id
	`, pattern, isRegex, targetModID, targetMachineID).Scan(
		&v.ID, &v.Pattern, &v.IsRegex, &v.TargetModID, &v.TargetMachineID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create valid recipe type: %w", err)
	}
	return v, nil
}

// DeleteValidRecipeType removes a valid recipe type by ID.
func (d *DB) DeleteValidRecipeType(ctx context.Context, id uuid.UUID) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM valid_recipe_types WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("db: delete valid recipe type: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ImportRecipe writes a single normalized recipe to the database in one transaction.
// It auto-creates any unknown mods, items, fluids, and tags referenced by the recipe.
// Returns imported=true if the recipe was new, false if a duplicate (same content_hash).
func (d *DB) ImportRecipe(ctx context.Context, rec model.NormalizedRecipe) (imported bool, err error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("db: import recipe: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Collect all mod namespaces referenced and upsert them.
	mods := collectMods(rec)
	for modID := range mods {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mods (mod_id, name, energy_type)
			VALUES ($1, $2, 'NONE')
			ON CONFLICT (mod_id) DO NOTHING
		`, modID, prettyName(modID)); err != nil {
			return false, fmt.Errorf("db: import recipe: upsert mod %s: %w", modID, err)
		}
	}

	// 1b. Upsert the machine type for this recipe.
	if _, err := tx.Exec(ctx, `
		INSERT INTO machine_types (mod_id, machine_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (mod_id, machine_id) DO NOTHING
	`, rec.ModID, rec.MachineID, prettyName(rec.MachineID)); err != nil {
		return false, fmt.Errorf("db: import recipe: upsert machine type: %w", err)
	}

	// 2. Upsert items referenced.
	for _, io := range rec.ItemInputs {
		if io.ItemModID != nil {
			if err := upsertItem(ctx, tx, *io.ItemModID, *io.ItemID); err != nil {
				return false, err
			}
		}
	}
	for _, io := range rec.ItemOutputs {
		if io.ItemModID != nil {
			if err := upsertItem(ctx, tx, *io.ItemModID, *io.ItemID); err != nil {
				return false, err
			}
		}
	}

	// 3. Upsert fluids referenced.
	for _, io := range rec.FluidInputs {
		if err := upsertFluid(ctx, tx, io.FluidModID, io.FluidID); err != nil {
			return false, err
		}
	}
	for _, io := range rec.FluidOutputs {
		if err := upsertFluid(ctx, tx, io.FluidModID, io.FluidID); err != nil {
			return false, err
		}
	}

	// 4. Upsert tags and build name→UUID map.
	tagIDs := make(map[string]uuid.UUID)
	for _, io := range rec.ItemInputs {
		if io.TagName == nil {
			continue
		}
		id, err := upsertTag(ctx, tx, *io.TagName)
		if err != nil {
			return false, err
		}
		tagIDs[*io.TagName] = id
	}

	// 5. Check for duplicate via content_hash.
	var existing uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&existing)
	if err == nil {
		// Duplicate — commit nothing, report as skipped.
		_ = tx.Rollback(ctx)
		return false, nil
	}
	if err != pgx.ErrNoRows {
		return false, fmt.Errorf("db: import recipe: check hash: %w", err)
	}

	// 6. Insert recipe row.
	var recipeID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO recipes
			(id, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu, priority, content_hash)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, 0, $6)
		RETURNING id
	`, rec.ModID, rec.MachineID, rec.Duration, rec.EUPerTick, int64(rec.Duration)*rec.EUPerTick, rec.ContentHash,
	).Scan(&recipeID); err != nil {
		return false, fmt.Errorf("db: import recipe: insert recipe: %w", err)
	}

	// 7. Insert item inputs.
	for _, io := range rec.ItemInputs {
		var tagID *uuid.UUID
		if io.TagName != nil {
			id := tagIDs[*io.TagName]
			tagID = &id
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_inputs
				(id, recipe_id, item_mod_id, item_id, tag_id,
				 amount_num, amount_den, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		`, recipeID, io.ItemModID, io.ItemID, tagID,
			io.AmountNum, io.AmountDen, io.ProbNum, io.ProbDen,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert item input: %w", err)
		}
	}

	// 8. Insert item outputs.
	for _, io := range rec.ItemOutputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_outputs
				(id, recipe_id, item_mod_id, item_id,
				 amount_num, amount_den, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		`, recipeID, *io.ItemModID, *io.ItemID,
			io.AmountNum, io.AmountDen, io.ProbNum, io.ProbDen,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert item output: %w", err)
		}
	}

	// 9. Insert fluid inputs.
	for _, io := range rec.FluidInputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_inputs
				(id, recipe_id, fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
		`, recipeID, io.FluidModID, io.FluidID, io.AmountMB, io.ProbNum, io.ProbDen,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert fluid input: %w", err)
		}
	}

	// 10. Insert fluid outputs.
	for _, io := range rec.FluidOutputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_outputs
				(id, recipe_id, fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
		`, recipeID, io.FluidModID, io.FluidID, io.AmountMB, io.ProbNum, io.ProbDen,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert fluid output: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("db: import recipe: commit: %w", err)
	}
	return true, nil
}

// prettyName converts a snake_case identifier to Title Case display name.
// "blast_furnace" → "Blast Furnace", "modern_industrialization" → "Modern Industrialization"
func prettyName(id string) string {
	words := strings.Split(id, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func upsertItem(ctx context.Context, tx pgx.Tx, modID, itemID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO items (mod_id, item_id, name, max_stack)
		VALUES ($1, $2, $3, 64)
		ON CONFLICT (mod_id, item_id) DO NOTHING
	`, modID, itemID, prettyName(itemID))
	if err != nil {
		return fmt.Errorf("db: upsert item %s:%s: %w", modID, itemID, err)
	}
	return nil
}

func upsertFluid(ctx context.Context, tx pgx.Tx, modID, fluidID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO fluids (mod_id, fluid_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (mod_id, fluid_id) DO NOTHING
	`, modID, fluidID, prettyName(fluidID))
	if err != nil {
		return fmt.Errorf("db: upsert fluid %s:%s: %w", modID, fluidID, err)
	}
	return nil
}

func upsertTag(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO tags (id, name)
		VALUES (gen_random_uuid(), $1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, name).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: upsert tag %q: %w", name, err)
	}
	return id, nil
}

// collectMods returns the set of all mod namespaces referenced by a recipe.
func collectMods(rec model.NormalizedRecipe) map[string]struct{} {
	mods := map[string]struct{}{
		rec.ModID: {},
	}
	add := func(modID *string) {
		if modID != nil {
			mods[*modID] = struct{}{}
		}
	}
	for _, io := range rec.ItemInputs {
		add(io.ItemModID)
	}
	for _, io := range rec.ItemOutputs {
		add(io.ItemModID)
	}
	for _, io := range rec.FluidInputs {
		mods[io.FluidModID] = struct{}{}
	}
	for _, io := range rec.FluidOutputs {
		mods[io.FluidModID] = struct{}{}
	}
	return mods
}
