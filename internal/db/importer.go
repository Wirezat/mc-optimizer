package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ImportRecipe writes a single normalized recipe to the database in one transaction.
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
			INSERT INTO mods (mod_id, name)
			VALUES ($1, $1)
			ON CONFLICT (mod_id) DO NOTHING
		`, modID); err != nil {
			return false, fmt.Errorf("db: import recipe: upsert mod %s: %w", modID, err)
		}
	}

	// 1b.
	var machineExists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM machine_types WHERE mod_id = $1 AND machine_id = $2)`,
		rec.ModID, rec.MachineID,
	).Scan(&machineExists); err != nil {
		return false, fmt.Errorf("db: import recipe: check machine type: %w", err)
	}
	if !machineExists {
		return false, fmt.Errorf("machine %s:%s is not declared by any mod", rec.ModID, rec.MachineID)
	}

	// 2. Upsert the items, fluids and tags the recipe references.
	tagIDs := make(map[tagKey]uuid.UUID)
	for _, io := range append(append([]resource.IO{}, rec.Inputs...), rec.Outputs...) {
		kind := io.Ref.Kind.Or()
		if io.Ref.TagRef != "" {
			k := tagKey{string(kind), io.Ref.TagRef}
			if _, ok := tagIDs[k]; ok {
				continue
			}
			id, err := upsertTag(ctx, tx, string(kind), io.Ref.TagRef)
			if err != nil {
				return false, err
			}
			tagIDs[k] = id
			continue
		}
		var err error
		switch kind {
		case resource.KindItem:
			err = upsertItem(ctx, tx, io.Ref.ModID, io.Ref.ID)
		case resource.KindFluid:
			err = upsertFluid(ctx, tx, io.Ref.ModID, io.Ref.ID)
		default:
			err = fmt.Errorf("db: import recipe: no table for kind %q", kind)
		}
		if err != nil {
			return false, err
		}
	}

	// 5.
	var existing uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&existing)
	if err == nil {
		existingModData, mErr := marshalModData(rec.ModData)
		if mErr != nil {
			return false, fmt.Errorf("db: import recipe: encode mod_data: %w", mErr)
		}
		tag, uErr := tx.Exec(ctx,
			`UPDATE recipes SET mod_data = $2 WHERE id = $1`, existing, existingModData)
		if uErr != nil {
			return false, fmt.Errorf("db: import recipe: refresh mod_data: %w", uErr)
		}
		if tag.RowsAffected() == 0 {
			return false, fmt.Errorf("db: import recipe: recipe %s vanished before its mod_data could be refreshed", existing)
		}
		if cErr := tx.Commit(ctx); cErr != nil {
			return false, fmt.Errorf("db: import recipe: commit mod_data refresh: %w", cErr)
		}
		return false, nil
	}
	if err != pgx.ErrNoRows {
		return false, fmt.Errorf("db: import recipe: check hash: %w", err)
	}

	// 6.
	var recipeID uuid.UUID
	var shape any
	if len(rec.Shape) > 0 {
		shape = rec.Shape
	}
	modData, err := marshalModData(rec.ModData)
	if err != nil {
		return false, fmt.Errorf("db: import recipe: encode mod_data: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO recipes
			(id, machine_mod_id, machine_id, source_mod_id, duration_ticks, mod_data, content_hash, shape)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, rec.ModID, rec.MachineID, rec.SourceModID, rec.Duration, modData, rec.ContentHash, shape,
	).Scan(&recipeID); err != nil {
		return false, fmt.Errorf("db: import recipe: insert recipe: %w", err)
	}

	// 7. Insert inputs and outputs, each kind into its table with its own sort order.
	if err := insertRecipeIO(ctx, tx, recipeID, rec.Inputs, false, tagIDs); err != nil {
		return false, err
	}
	if err := insertRecipeIO(ctx, tx, recipeID, rec.Outputs, true, tagIDs); err != nil {
		return false, err
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

// insertRecipeIO writes ios into the input or output table of each io's kind.
func insertRecipeIO(ctx context.Context, tx pgx.Tx, recipeID uuid.UUID, ios []resource.IO, output bool, tagIDs map[tagKey]uuid.UUID) error {
	side := "input"
	if output {
		side = "output"
	}
	next := map[resource.Kind]int{}
	for _, io := range ios {
		kind := io.Ref.Kind.Or()
		i := next[kind]
		next[kind]++
		var tagID *uuid.UUID
		if io.Ref.TagRef != "" {
			id := tagIDs[tagKey{string(kind), io.Ref.TagRef}]
			tagID = &id
		}
		var err error
		switch {
		case kind == resource.KindItem && output:
			if tagID != nil {
				return fmt.Errorf("db: import recipe: output %d is a tag, which cannot be produced", i)
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO recipe_item_outputs
					(id, recipe_id, sort_index, item_mod_id, item_id,
					 amount_num, amount_den, probability_num, probability_den)
				VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
			`, recipeID, i, io.Ref.ModID, io.Ref.ID, io.Amount.Num, io.Amount.Den, io.Prob.Num, io.Prob.Den)
		case kind == resource.KindItem:
			_, err = tx.Exec(ctx, `
				INSERT INTO recipe_item_inputs
					(id, recipe_id, sort_index, item_mod_id, item_id, tag_id,
					 amount_num, amount_den, probability_num, probability_den, non_consuming)
				VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			`, recipeID, i, nullIfEmpty(io.Ref.ModID), nullIfEmpty(io.Ref.ID), tagID,
				io.Amount.Num, io.Amount.Den, io.Prob.Num, io.Prob.Den, !io.Consumed)
		case kind == resource.KindFluid:
			if io.Amount.Den != 1 {
				return fmt.Errorf("db: import recipe: fluid amount %s is not whole mB", io.Amount)
			}
			if output {
				_, err = tx.Exec(ctx, `
					INSERT INTO recipe_fluid_outputs
						(id, recipe_id, sort_index, fluid_mod_id, fluid_id, tag_id, amount_mb, probability_num, probability_den)
					VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
				`, recipeID, i, nullIfEmpty(io.Ref.ModID), nullIfEmpty(io.Ref.ID), tagID, io.Amount.Num, io.Prob.Num, io.Prob.Den)
				break
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO recipe_fluid_inputs
					(id, recipe_id, sort_index, fluid_mod_id, fluid_id, tag_id, amount_mb, probability_num, probability_den, non_consuming)
				VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9)
			`, recipeID, i, nullIfEmpty(io.Ref.ModID), nullIfEmpty(io.Ref.ID), tagID, io.Amount.Num, io.Prob.Num, io.Prob.Den, !io.Consumed)
		default:
			return fmt.Errorf("db: import recipe: no table for kind %q", kind)
		}
		if err != nil {
			return fmt.Errorf("db: import recipe: insert %s %s: %w", kind, side, err)
		}
	}
	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type tagKey struct{ kind, name string }

func upsertTag(ctx context.Context, tx pgx.Tx, kind, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO tags (id, kind, name)
		VALUES (gen_random_uuid(), $1, $2)
		ON CONFLICT (kind, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, kind, name).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: upsert %s tag %q: %w", kind, name, err)
	}
	return id, nil
}

// BulkUpsertItems inserts or updates items from a mod definition, each with its own curated
// max_stack (yaml `max_stack`, default 64).
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

// UpsertBlockDrops inserts block drop records.
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
		// The whole second slot is either present or absent; a partially filled one would trip
		// the table's own (mod IS NULL) = (item IS NULL) check.
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

func collectMods(rec model.NormalizedRecipe) map[string]struct{} {
	mods := map[string]struct{}{
		rec.ModID: {},
	}
	for _, io := range append(append([]resource.IO{}, rec.Inputs...), rec.Outputs...) {
		if io.Ref.ModID != "" {
			mods[io.Ref.ModID] = struct{}{}
		}
	}
	return mods
}
