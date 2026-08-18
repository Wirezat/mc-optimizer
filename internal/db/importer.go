package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
			VALUES ($1, $1, 'NONE')
			ON CONFLICT (mod_id) DO NOTHING
		`, modID); err != nil {
			return false, fmt.Errorf("db: import recipe: upsert mod %s: %w", modID, err)
		}
	}

	// 1b. Upsert the machine type for this recipe.
	if _, err := tx.Exec(ctx, `
		INSERT INTO machine_types (mod_id, machine_id, name)
		VALUES ($1, $2, $2)
		ON CONFLICT (mod_id, machine_id) DO NOTHING
	`, rec.ModID, rec.MachineID); err != nil {
		return false, fmt.Errorf("db: import recipe: upsert machine type: %w", err)
	}

	// 2. Upsert items referenced.
	for _, io := range rec.ItemInputs {
		if io.ModID != nil {
			if err := upsertItem(ctx, tx, *io.ModID, *io.ID); err != nil {
				return false, err
			}
		}
	}
	for _, io := range rec.ItemOutputs {
		if io.ModID != nil {
			if err := upsertItem(ctx, tx, *io.ModID, *io.ID); err != nil {
				return false, err
			}
		}
	}

	// 3. Upsert fluids referenced (skip tag-only references).
	for _, io := range rec.FluidInputs {
		if io.ModID != nil {
			if err := upsertFluid(ctx, tx, *io.ModID, *io.ID); err != nil {
				return false, err
			}
		}
	}
	for _, io := range rec.FluidOutputs {
		if io.ModID != nil {
			if err := upsertFluid(ctx, tx, *io.ModID, *io.ID); err != nil {
				return false, err
			}
		}
	}

	// 4. Upsert tags and build name→UUID map.
	tagIDs := make(map[string]uuid.UUID)
	upsertTagInto := func(name string) error {
		if _, ok := tagIDs[name]; ok {
			return nil
		}
		id, err := upsertTag(ctx, tx, name)
		if err != nil {
			return err
		}
		tagIDs[name] = id
		return nil
	}
	for _, io := range rec.ItemInputs {
		if io.TagName != nil {
			if err := upsertTagInto(*io.TagName); err != nil {
				return false, err
			}
		}
	}
	for _, io := range rec.FluidInputs {
		if io.TagName != nil {
			if err := upsertTagInto(*io.TagName); err != nil {
				return false, err
			}
		}
	}
	for _, io := range rec.FluidOutputs {
		if io.TagName != nil {
			if err := upsertTagInto(*io.TagName); err != nil {
				return false, err
			}
		}
	}

	// 5. Check for duplicate via content_hash.
	var existing uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&existing)
	if err == nil {
		_ = tx.Rollback(ctx)
		return false, nil
	}
	if err != pgx.ErrNoRows {
		return false, fmt.Errorf("db: import recipe: check hash: %w", err)
	}

	// 6. Insert recipe row.
	var recipeID uuid.UUID
	var shape any
	if len(rec.Shape) > 0 {
		shape = rec.Shape
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO recipes
			(id, machine_mod_id, machine_id, source_mod_id, duration_ticks, eu_per_tick, total_eu, content_hash, shape)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, rec.ModID, rec.MachineID, rec.SourceModID, rec.Duration, rec.EUPerTick, int64(rec.Duration)*rec.EUPerTick, rec.ContentHash, shape,
	).Scan(&recipeID); err != nil {
		return false, fmt.Errorf("db: import recipe: insert recipe: %w", err)
	}

	// 7. Insert item inputs.
	for i, io := range rec.ItemInputs {
		var tagID *uuid.UUID
		if io.TagName != nil {
			id := tagIDs[*io.TagName]
			tagID = &id
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_inputs
				(id, recipe_id, sort_index, item_mod_id, item_id, tag_id,
				 amount_num, amount_den, probability_num, probability_den, non_consuming)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, recipeID, i, io.ModID, io.ID, tagID,
			io.AmountNum, io.AmountDen, io.ProbNum, io.ProbDen, io.NonConsuming,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert item input: %w", err)
		}
	}

	// 8. Insert item outputs.
	for i, io := range rec.ItemOutputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_outputs
				(id, recipe_id, sort_index, item_mod_id, item_id,
				 amount_num, amount_den, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		`, recipeID, i, *io.ModID, *io.ID,
			io.AmountNum, io.AmountDen, io.ProbNum, io.ProbDen,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert item output: %w", err)
		}
	}

	// 9. Insert fluid inputs.
	for i, io := range rec.FluidInputs {
		var tagID *uuid.UUID
		if io.TagName != nil {
			id := tagIDs[*io.TagName]
			tagID = &id
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_inputs
				(id, recipe_id, sort_index, fluid_mod_id, fluid_id, tag_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		`, recipeID, i, io.ModID, io.ID, tagID, io.AmountMB, io.ProbNum, io.ProbDen,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert fluid input: %w", err)
		}
	}

	// 10. Insert fluid outputs.
	for i, io := range rec.FluidOutputs {
		var tagID *uuid.UUID
		if io.TagName != nil {
			id := tagIDs[*io.TagName]
			tagID = &id
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_outputs
				(id, recipe_id, sort_index, fluid_mod_id, fluid_id, tag_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		`, recipeID, i, io.ModID, io.ID, tagID, io.AmountMB, io.ProbNum, io.ProbDen,
		); err != nil {
			return false, fmt.Errorf("db: import recipe: insert fluid output: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("db: import recipe: commit: %w", err)
	}
	return true, nil
}

// UpsertTranslations upserts lang key→name entries for a given language code.
func (d *DB) UpsertTranslations(ctx context.Context, lang string, entries map[string]string) error {
	if len(entries) == 0 {
		return nil
	}
	keys := make([]string, 0, len(entries))
	vals := make([]string, 0, len(entries))
	for k, v := range entries {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO translations (lang, lang_key, name)
		SELECT $1, k, v FROM unnest($2::text[], $3::text[]) AS t(k, v)
		ON CONFLICT (lang, lang_key) DO UPDATE SET name = EXCLUDED.name
	`, lang, keys, vals)
	if err != nil {
		return fmt.Errorf("db: upsert translations: %w", err)
	}
	return nil
}

// UpsertTagValues stores raw tag values for a given tag name.
// If replace=true, all existing values for that tag are deleted first.
func (d *DB) UpsertTagValues(ctx context.Context, tagName string, values []string, replace bool) error {
	if len(values) == 0 && !replace {
		return nil
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: upsert tag values: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if replace {
		if _, err := tx.Exec(ctx, `DELETE FROM tag_values WHERE tag_name = $1`, tagName); err != nil {
			return fmt.Errorf("db: upsert tag values: delete: %w", err)
		}
	}
	if len(values) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tag_values (tag_name, value)
			SELECT $1, unnest($2::text[])
			ON CONFLICT DO NOTHING
		`, tagName, values); err != nil {
			return fmt.Errorf("db: upsert tag values: insert: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// LoadAllTagValues loads all raw tag values from the DB, returning map[tagName][]value.
func (d *DB) LoadAllTagValues(ctx context.Context) (map[string][]string, error) {
	rows, err := d.Pool.Query(ctx, `SELECT tag_name, value FROM tag_values ORDER BY tag_name, value`)
	if err != nil {
		return nil, fmt.Errorf("db: load all tag values: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var tagName, value string
		if err := rows.Scan(&tagName, &value); err != nil {
			return nil, fmt.Errorf("db: load all tag values: scan: %w", err)
		}
		result[tagName] = append(result[tagName], value)
	}
	return result, rows.Err()
}

// UpsertTagMembers writes the fully resolved tag→items mapping.
// For each tag, existing members are replaced with the resolved list.
// Only items that exist in the items table are inserted.
func (d *DB) UpsertTagMembers(ctx context.Context, resolved map[string][]string) error {
	if len(resolved) == 0 {
		return nil
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: upsert tag members: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for tagName, items := range resolved {
		var tagID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO tags (id, name) VALUES (gen_random_uuid(), $1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, tagName).Scan(&tagID); err != nil {
			return fmt.Errorf("db: upsert tag members: upsert tag %q: %w", tagName, err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM tag_members WHERE tag_id = $1`, tagID); err != nil {
			return fmt.Errorf("db: upsert tag members: delete old members for %q: %w", tagName, err)
		}

		if len(items) == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO tag_members (tag_id, item_mod_id, item_id)
			SELECT $1, split_part(v, ':', 1), split_part(v, ':', 2)
			FROM unnest($2::text[]) AS v
			WHERE EXISTS (
				SELECT 1 FROM items i
				WHERE i.mod_id  = split_part(v, ':', 1)
				  AND i.item_id = split_part(v, ':', 2)
			)
			ON CONFLICT DO NOTHING
		`, tagID, items); err != nil {
			return fmt.Errorf("db: upsert tag members: insert for %q: %w", tagName, err)
		}
	}

	return tx.Commit(ctx)
}

func upsertItem(ctx context.Context, tx pgx.Tx, modID, itemID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO items (mod_id, item_id, max_stack)
		VALUES ($1, $2, 64)
		ON CONFLICT (mod_id, item_id) DO NOTHING
	`, modID, itemID)
	if err != nil {
		return fmt.Errorf("db: upsert item %s:%s: %w", modID, itemID, err)
	}
	return nil
}

func upsertFluid(ctx context.Context, tx pgx.Tx, modID, fluidID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO fluids (mod_id, fluid_id)
		VALUES ($1, $2)
		ON CONFLICT (mod_id, fluid_id) DO NOTHING
	`, modID, fluidID)
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

// UpdateModDisplayNames updates the display name for each mod in the map.
// Only updates rows where name currently equals mod_id (the default), preserving manually set names.
func (d *DB) UpdateModDisplayNames(ctx context.Context, names map[string]string) error {
	for modID, displayName := range names {
		if displayName == "" || displayName == modID {
			continue
		}
		_, err := d.Pool.Exec(ctx, `
			UPDATE mods SET name = $2
			WHERE mod_id = $1 AND name = $1
		`, modID, displayName)
		if err != nil {
			return fmt.Errorf("db: update mod display name %s: %w", modID, err)
		}
	}
	return nil
}

// BulkUpsertItems inserts (or updates) items extracted from a mod definition, with
// each item's own curated max_stack (yaml `max_stack`, default 64 if unspecified —
// NOT hardcoded 64 regardless of curation, that was a bug: any yaml-curated max_stack
// was silently discarded and reimports never updated an already-existing row).
// The mod must already exist; items for unknown mods are silently skipped.
// IDs that already exist in the fluids table are also skipped — block.* lang keys
// are used for both items and fluids, so without this check fluids would be double-registered.
func (d *DB) BulkUpsertItems(ctx context.Context, modID string, items []model.ItemDef) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	maxStacks := make([]int, len(items))
	for i, it := range items {
		ids[i] = it.ItemID
		maxStacks[i] = it.MaxStack
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO items (mod_id, item_id, max_stack)
		SELECT $1, t.id, t.max_stack FROM unnest($2::text[], $3::int[]) AS t(id, max_stack)
		WHERE EXISTS (SELECT 1 FROM mods WHERE mod_id = $1)
		  AND NOT EXISTS (SELECT 1 FROM fluids WHERE mod_id = $1 AND fluid_id = t.id)
		ON CONFLICT (mod_id, item_id) DO UPDATE SET max_stack = EXCLUDED.max_stack
	`, modID, ids, maxStacks)
	if err != nil {
		return fmt.Errorf("db: bulk upsert items for mod %s: %w", modID, err)
	}
	return nil
}

// MirrorFluidTranslations creates fluid.* translation keys from matching block.* keys.
// Needed because lang files use block.{ns}.{id} for fluids, but the search layer expects fluid.{ns}.{id}.
func (d *DB) MirrorFluidTranslations(ctx context.Context) error {
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO translations (lang, lang_key, name)
		SELECT t.lang,
		       'fluid.' || f.mod_id || '.' || f.fluid_id,
		       t.name
		FROM translations t
		JOIN fluids f ON f.mod_id   = split_part(t.lang_key, '.', 2)
		             AND f.fluid_id = split_part(t.lang_key, '.', 3)
		WHERE t.lang_key LIKE 'block.%.%'
		  AND split_part(t.lang_key, '.', 1) = 'block'
		ON CONFLICT (lang, lang_key) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("db: mirror fluid translations: %w", err)
	}
	return nil
}

// UpsertBlockDrops inserts block drop records. Both block and drop items must exist.
// Drops for item pairs not in the items table are silently skipped via FK-safe insert.
func (d *DB) UpsertBlockDrops(ctx context.Context, drops []model.BlockDrop) error {
	if len(drops) == 0 {
		return nil
	}
	blockMods := make([]string, len(drops))
	blockItems := make([]string, len(drops))
	dropMods := make([]string, len(drops))
	dropItems := make([]string, len(drops))
	mins := make([]int, len(drops))
	maxs := make([]int, len(drops))
	conds := make([]string, len(drops))
	for i, d := range drops {
		blockMods[i] = d.BlockModID
		blockItems[i] = d.BlockItemID
		dropMods[i] = d.DropModID
		dropItems[i] = d.DropItemID
		mins[i] = d.MinCount
		maxs[i] = d.MaxCount
		conds[i] = d.Condition
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO block_drops
			(block_mod_id, block_item_id, drop_mod_id, drop_item_id, min_count, max_count, condition)
		SELECT bm, bi, dm, di, mn, mx, co
		FROM unnest(
			$1::text[], $2::text[], $3::text[], $4::text[],
			$5::int[], $6::int[], $7::text[]
		) AS t(bm, bi, dm, di, mn, mx, co)
		WHERE EXISTS (SELECT 1 FROM items WHERE mod_id = bm AND item_id = bi)
		  AND EXISTS (SELECT 1 FROM items WHERE mod_id = dm AND item_id = di)
		ON CONFLICT DO NOTHING
	`, blockMods, blockItems, dropMods, dropItems, mins, maxs, conds)
	if err != nil {
		return fmt.Errorf("db: upsert block drops: %w", err)
	}
	return nil
}

// UpsertVillagerTrades inserts villager trade records.
// Trades whose cost or result item do not exist in the items table are silently skipped.
func (d *DB) UpsertVillagerTrades(ctx context.Context, trades []model.VillagerTrade) error {
	if len(trades) == 0 {
		return nil
	}
	srcMods := make([]string, len(trades))
	keys := make([]string, len(trades))
	profs := make([]string, len(trades))
	tiers := make([]int, len(trades))
	costMods := make([]string, len(trades))
	costItems := make([]string, len(trades))
	costCounts := make([]int, len(trades))
	cost2Mods := make([]*string, len(trades))
	cost2Items := make([]*string, len(trades))
	cost2Counts := make([]*int, len(trades))
	resMods := make([]string, len(trades))
	resItems := make([]string, len(trades))
	resCounts := make([]int, len(trades))
	modified := make([]bool, len(trades))
	variable := make([]bool, len(trades))
	maxUses := make([]*int, len(trades))
	xps := make([]*int, len(trades))
	for i, t := range trades {
		srcMods[i] = t.SourceModID
		keys[i] = t.TradeKey
		profs[i] = t.Profession
		tiers[i] = t.Tier
		costMods[i] = t.CostModID
		costItems[i] = t.CostItemID
		costCounts[i] = t.CostCount
		// The whole second slot is either present or absent; a partially filled
		// one would trip the table's own (mod IS NULL) = (item IS NULL) check.
		if t.Cost2ModID != "" && t.Cost2ItemID != "" {
			mod, item, count := t.Cost2ModID, t.Cost2ItemID, t.Cost2Count
			if count < 1 {
				count = 1
			}
			cost2Mods[i], cost2Items[i], cost2Counts[i] = &mod, &item, &count
		}
		resMods[i] = t.ResultModID
		resItems[i] = t.ResultItemID
		resCounts[i] = t.ResultCount
		modified[i] = t.ResultModified
		variable[i] = t.CostVariable
		maxUses[i] = t.MaxUses
		xps[i] = t.XP
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO villager_trades
			(source_mod_id, trade_key, profession, tier,
			 cost_mod_id, cost_item_id, cost_count,
			 cost2_mod_id, cost2_item_id, cost2_count,
			 result_mod_id, result_item_id, result_count, result_modified,
			 cost_variable, max_uses, xp)
		SELECT sm, tk, pr, ti, cm, ci, cc, c2m, c2i, c2c, rm, ri, rc, mo, cv, mu, xp
		FROM unnest(
			$1::text[], $2::text[], $3::text[], $4::int[],
			$5::text[], $6::text[], $7::int[],
			$8::text[], $9::text[], $10::int[],
			$11::text[], $12::text[], $13::int[], $14::bool[], $15::bool[],
			$16::int[], $17::int[]
		) AS t(sm, tk, pr, ti, cm, ci, cc, c2m, c2i, c2c, rm, ri, rc, mo, cv, mu, xp)
		WHERE EXISTS (SELECT 1 FROM mods  WHERE mod_id = sm)
		  AND EXISTS (SELECT 1 FROM items WHERE mod_id = cm AND item_id = ci)
		  AND EXISTS (SELECT 1 FROM items WHERE mod_id = rm AND item_id = ri)
		  -- An absent second slot must not disqualify the offer, only an
		  -- unknown one.
		  AND (c2m IS NULL OR EXISTS (SELECT 1 FROM items WHERE mod_id = c2m AND item_id = c2i))
		ON CONFLICT (source_mod_id, profession, tier, trade_key) DO NOTHING
	`, srcMods, keys, profs, tiers,
		costMods, costItems, costCounts,
		cost2Mods, cost2Items, cost2Counts,
		resMods, resItems, resCounts, modified, variable, maxUses, xps)
	if err != nil {
		return fmt.Errorf("db: upsert villager trades: %w", err)
	}
	return nil
}

// UpsertUpgradeTiers inserts upgrade tier records from machine_upgrades.json datamaps.
// Entries whose mod_id does not exist in the mods table are silently skipped.
func (d *DB) UpsertUpgradeTiers(ctx context.Context, tiers []model.UpgradeTier) error {
	if len(tiers) == 0 {
		return nil
	}
	modIDs := make([]string, len(tiers))
	names := make([]string, len(tiers))
	bonuses := make([]int64, len(tiers))
	itemModIDs := make([]string, len(tiers))
	itemIDs := make([]string, len(tiers))
	for i, t := range tiers {
		modIDs[i] = t.ModID
		names[i] = t.Name
		bonuses[i] = t.EUBonusPerSlot
		itemModIDs[i] = t.ItemModID
		itemIDs[i] = t.ItemID
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO upgrade_tiers (mod_id, name, eu_bonus_per_slot, item_ref)
		SELECT mid, nm, bonus, imod || ':' || iid
		FROM unnest($1::text[], $2::text[], $3::bigint[], $4::text[], $5::text[]) AS t(mid, nm, bonus, imod, iid)
		WHERE EXISTS (SELECT 1 FROM mods WHERE mod_id = mid)
		ON CONFLICT (item_ref) DO UPDATE
		  SET name = EXCLUDED.name,
		      eu_bonus_per_slot = EXCLUDED.eu_bonus_per_slot
	`, modIDs, names, bonuses, itemModIDs, itemIDs)
	if err != nil {
		return fmt.Errorf("db: upsert upgrade tiers: %w", err)
	}
	return nil
}

// LocalizeMachineNames resolves a translation key for each machine_type and stores it in
// name_lang_key. The name column is set to initcap(machine_id) as a language-neutral fallback
// (used when the key cannot be found at query time). Resolution priority for the key:
//  1. Exact block.{mod}.{machine_id}
//  2. Exact rei_categories.{mod}.{machine_id}
//  3. rei_categories.{mod}.{tier}_{machine_id} where lower(name) = replace(machine_id,'_',' ')
//  4. block.{mod}.{tier}_{machine_id} with the same name check
//
// name_lang_key = NULL means no key found; callers fall back to name.
// Idempotent — safe to call on every import.
func (d *DB) LocalizeMachineNames(ctx context.Context) error {
	_, err := d.Pool.Exec(ctx, `
		UPDATE machine_types mt
		SET
			name_lang_key = COALESCE(
				-- 1. exact block.* key
				(SELECT t.lang_key FROM translations t
				 WHERE t.lang = 'en_us'
				   AND t.lang_key = 'block.' || mt.mod_id || '.' || mt.machine_id
				 LIMIT 1),
				-- 2. exact rei_categories.* key
				(SELECT t.lang_key FROM translations t
				 WHERE t.lang = 'en_us'
				   AND t.lang_key = 'rei_categories.' || mt.mod_id || '.' || mt.machine_id
				 LIMIT 1),
				-- 3. rei_categories.{mod}.{tier}_{machine_id} where name matches generic form
				(SELECT t.lang_key FROM translations t
				 WHERE t.lang = 'en_us'
				   AND t.lang_key LIKE 'rei_categories.' || mt.mod_id || '.%' || mt.machine_id
				   AND lower(t.name) = replace(mt.machine_id, '_', ' ')
				 LIMIT 1),
				-- 4. block.{mod}.{tier}_{machine_id} with same name check
				(SELECT t.lang_key FROM translations t
				 WHERE t.lang = 'en_us'
				   AND t.lang_key LIKE 'block.' || mt.mod_id || '.%' || mt.machine_id
				   AND lower(t.name) = replace(mt.machine_id, '_', ' ')
				 LIMIT 1)
			),
			name = initcap(replace(mt.machine_id, '_', ' '))
		WHERE mt.name = mt.machine_id OR mt.name_lang_key IS NULL
	`)
	if err != nil {
		return fmt.Errorf("db: localize machine names: %w", err)
	}
	return nil
}

// SetMIEnergyType sets energy_type = 'eu' for all machine_types belonging to the given mods.
// Must be called before SetMachinesUpgradable so the upgradable filter can match.
// Idempotent — safe to call on every import.
func (d *DB) SetMIEnergyType(ctx context.Context, modIDs []string) error {
	if len(modIDs) == 0 {
		return nil
	}
	_, err := d.Pool.Exec(ctx, `
		UPDATE machine_types
		SET energy_type = 'eu'
		WHERE mod_id = ANY($1)
	`, modIDs)
	if err != nil {
		return fmt.Errorf("db: set MI energy type: %w", err)
	}
	return nil
}

// SetMISteamMachines overrides energy_type to 'steam' and clears upgradable for the given
// machine IDs within the given mods. Must be called after SetMIEnergyType so it can
// selectively undo the EU assignment for steam-only machines (coke_oven, steam_blast_furnace).
// Idempotent — safe to call on every import.
func (d *DB) SetMISteamMachines(ctx context.Context, modIDs []string, machineIDs []string) error {
	if len(modIDs) == 0 || len(machineIDs) == 0 {
		return nil
	}
	_, err := d.Pool.Exec(ctx, `
		UPDATE machine_types
		SET energy_type = 'steam', upgradable = false
		WHERE mod_id = ANY($1) AND machine_id = ANY($2)
	`, modIDs, machineIDs)
	if err != nil {
		return fmt.Errorf("db: set MI steam machines: %w", err)
	}
	return nil
}

// SetMachinesUpgradable marks all EU machines belonging to the given mods as upgradable.
// Idempotent — safe to call on every import.
func (d *DB) SetMachinesUpgradable(ctx context.Context, modIDs []string) error {
	if len(modIDs) == 0 {
		return nil
	}
	_, err := d.Pool.Exec(ctx, `
		UPDATE machine_types
		SET upgradable = true
		WHERE mod_id = ANY($1) AND energy_type = 'eu'
	`, modIDs)
	if err != nil {
		return fmt.Errorf("db: set machines upgradable: %w", err)
	}
	return nil
}

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
		add(io.ModID)
	}
	for _, io := range rec.ItemOutputs {
		add(io.ModID)
	}
	for _, io := range rec.FluidInputs {
		add(io.ModID)
	}
	for _, io := range rec.FluidOutputs {
		add(io.ModID)
	}
	return mods
}
