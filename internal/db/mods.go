package db

import (
	"context"
	"fmt"
	"sort"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

// strPtrOr returns a pointer to s if non-empty, else nil.
func strPtrOr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// LookupItemNames resolves en_us display names for a batch of items/fluids.
func (d *DB) LookupItemNames(ctx context.Context, items []solver.ItemRef) (map[string]string, error) {
	type cand struct {
		langKey, itemKey string
		pri              int
	}
	var cands []cand
	seen := make(map[string]bool)
	for _, item := range items {
		if item.TagRef != "" {
			continue
		}
		k := item.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		base := item.ModID + "." + item.ItemID
		if item.IsFluid {
			cands = append(cands, cand{"fluid." + base, k, 0})
			cands = append(cands, cand{"block." + base, k, 1})
		} else {
			cands = append(cands, cand{"item." + base, k, 0})
			cands = append(cands, cand{"block." + base, k, 1})
		}
	}
	if len(cands) == 0 {
		return map[string]string{}, nil
	}

	langKeys := make([]string, len(cands))
	for i, c := range cands {
		langKeys[i] = c.langKey
	}
	rows, err := d.Pool.Query(ctx,
		`SELECT lang_key, name FROM translations WHERE lang = 'en_us' AND lang_key = ANY($1::text[])`,
		langKeys)
	if err != nil {
		return nil, fmt.Errorf("db: lookup item names: %w", err)
	}
	defer rows.Close()

	candMap := make(map[string]cand, len(cands))
	for _, c := range cands {
		candMap[c.langKey] = c
	}
	type bestEntry struct {
		name string
		pri  int
	}
	bestMap := make(map[string]bestEntry)
	for rows.Next() {
		var langKey, name string
		if err := rows.Scan(&langKey, &name); err != nil {
			return nil, fmt.Errorf("db: lookup item names: scan: %w", err)
		}
		c, ok := candMap[langKey]
		if !ok {
			continue
		}
		if cur, exists := bestMap[c.itemKey]; !exists || c.pri < cur.pri {
			bestMap[c.itemKey] = bestEntry{name, c.pri}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: lookup item names: %w", err)
	}
	result := make(map[string]string, len(bestMap))
	for k, b := range bestMap {
		result[k] = b.name
	}
	return result, nil
}

// LookupMachineNames resolves display names for a set of (modID, machineID) pairs. Returns
// a map[modID+":"+machineID → name].
func (d *DB) LookupMachineNames(ctx context.Context, machines []solver.MachineRef) (map[string]string, error) {
	if len(machines) == 0 {
		return map[string]string{}, nil
	}
	modIDs := make([]string, len(machines))
	machineIDs := make([]string, len(machines))
	for i, m := range machines {
		modIDs[i] = m.ModID
		machineIDs[i] = m.MachineID
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT mt.mod_id, mt.machine_id,
		       mt.name
		FROM machine_types mt
		WHERE (mt.mod_id, mt.machine_id) IN (SELECT unnest($1::text[]), unnest($2::text[]))
	`, modIDs, machineIDs)
	if err != nil {
		return nil, fmt.Errorf("db: lookup machine names: %w", err)
	}
	defer rows.Close()
	result := make(map[string]string, len(machines))
	for rows.Next() {
		var modID, machineID, name string
		if err := rows.Scan(&modID, &machineID, &name); err != nil {
			return nil, fmt.Errorf("db: lookup machine names: scan: %w", err)
		}
		result[modID+":"+machineID] = name
	}
	return result, rows.Err()
}

// LookupMachinePluginMods resolves, for a set of (modID, machineID) pairs, the mod whose
// plugin evaluates that machine: its ecosystem, or its own mod when no ecosystem is set.
// Returns a map[modID+":"+machineID → pluginModID].
func (d *DB) LookupMachinePluginMods(ctx context.Context, machines []solver.MachineRef) (map[string]string, error) {
	if len(machines) == 0 {
		return map[string]string{}, nil
	}
	modIDs := make([]string, len(machines))
	machineIDs := make([]string, len(machines))
	for i, m := range machines {
		modIDs[i] = m.ModID
		machineIDs[i] = m.MachineID
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT mt.mod_id, mt.machine_id, COALESCE(NULLIF(mt.ecosystem, ''), mt.mod_id)
		FROM machine_types mt
		WHERE (mt.mod_id, mt.machine_id) IN (SELECT unnest($1::text[]), unnest($2::text[]))
		  AND COALESCE(mt.ecosystem, '') <> $3
	`, modIDs, machineIDs, solver.VanillaEcosystem)
	if err != nil {
		return nil, fmt.Errorf("db: lookup machine plugin mods: %w", err)
	}
	defer rows.Close()
	result := make(map[string]string, len(machines))
	for rows.Next() {
		var modID, machineID, pluginMod string
		if err := rows.Scan(&modID, &machineID, &pluginMod); err != nil {
			return nil, fmt.Errorf("db: lookup machine plugin mods: scan: %w", err)
		}
		result[modID+":"+machineID] = pluginMod
	}
	return result, rows.Err()
}

// ListMods returns all mods ordered by name, including optional Modrinth metadata.
func (d *DB) ListMods(ctx context.Context) ([]*model.Mod, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT m.mod_id, m.name,
		       m.description, m.author, m.license,
		       m.url_source, m.url_modrinth, m.url_wiki, m.url_issues, m.url_discord,
		       m.modrinth_slug,
		       (SELECT COUNT(*) FROM recipes r WHERE r.source_mod_id = m.mod_id) AS recipe_count,
		       (SELECT COUNT(*) FROM items i WHERE i.mod_id = m.mod_id) AS item_count,
		       COALESCE((SELECT array_agg(DISTINCT COALESCE(NULLIF(mt.ecosystem, ''), mt.mod_id))
		                 FROM machine_types mt WHERE mt.mod_id = m.mod_id), '{}') AS ecosystems
		FROM mods m
		ORDER BY m.name
	`)
	if err != nil {
		return nil, fmt.Errorf("db: list mods: %w", err)
	}
	defer rows.Close()

	var mods []*model.Mod
	for rows.Next() {
		m := &model.Mod{}
		if err := rows.Scan(
			&m.ModID, &m.Name,
			&m.Description, &m.Author, &m.License,
			&m.URLSource, &m.URLModrinth, &m.URLWiki, &m.URLIssues, &m.URLDiscord,
			&m.ModrinthSlug, &m.RecipeCount, &m.ItemCount, &m.Ecosystems,
		); err != nil {
			return nil, fmt.Errorf("db: scan mod: %w", err)
		}
		mods = append(mods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list mods: %w", err)
	}
	return mods, nil
}

// UpdateModFull overwrites all editable fields for a mod.
func (d *DB) UpdateModFull(ctx context.Context, modID string, u model.ModUpdate) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE mods SET
			name          = COALESCE($2,  name),
			description   = $3,
			author        = $4,
			license       = $5,
			url_source    = $6,
			url_modrinth  = $7,
			url_wiki      = $8,
			url_issues    = $9,
			url_discord   = $10,
			modrinth_slug = $11
		WHERE mod_id = $1
	`, modID, u.Name,
		u.Description, u.Author, u.License,
		u.URLSource, u.URLModrinth, u.URLWiki, u.URLIssues,
		u.URLDiscord, u.ModrinthSlug)
	if err != nil {
		return fmt.Errorf("db: update mod full: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMachineType updates the display name of a machine type.
func (d *DB) UpdateMachineType(ctx context.Context, modID, machineID string, name *string) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_types
		SET name = COALESCE($3, name)
		WHERE mod_id = $1 AND machine_id = $2
	`, modID, machineID, name)
	if err != nil {
		return fmt.Errorf("db: update machine type: %w", err)
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
		SELECT i.mod_id, i.item_id,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		           ''
		       ),
		       i.max_stack
		FROM items i
		WHERE i.mod_id = $1
		ORDER BY i.item_id
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

// ListItemsProducedBy returns every item that appears as an output of a recipe matching
// (modID, machineID), same filter shape as ListRecipesCatalog: machineID set narrows to
// that exact machine (plus anything that implements it via machine_interfaces); machineID
// empty narrows to recipes the mod itself added (source_mod_id); both empty returns nothing
// (callers only call this with at least one set).
func (d *DB) ListItemsProducedBy(ctx context.Context, modID, machineID string) ([]*model.Item, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT DISTINCT i.mod_id, i.item_id,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		           ''
		       ) AS iname,
		       i.max_stack
		FROM items i
		JOIN recipe_item_outputs rio ON rio.item_mod_id = i.mod_id AND rio.item_id = i.item_id
		JOIN recipes r ON r.id = rio.recipe_id
		WHERE ( ($2 <> '' AND r.machine_mod_id = $1 AND r.machine_id = $2)
		     OR ($2 = ''  AND $1 <> '' AND r.source_mod_id = $1) )

		UNION

		SELECT DISTINCT i.mod_id, i.item_id,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		           ''
		       ) AS iname,
		       i.max_stack
		FROM items i
		JOIN recipe_item_outputs rio ON rio.item_mod_id = i.mod_id AND rio.item_id = i.item_id
		JOIN recipes r ON r.id = rio.recipe_id
		JOIN machine_interfaces mi ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
		WHERE $2 <> '' AND mi.machine_mod_id = $1 AND mi.machine_id = $2

		ORDER BY iname
	`, modID, machineID)
	if err != nil {
		return nil, fmt.Errorf("db: list items produced by: %w", err)
	}
	defer rows.Close()

	var items []*model.Item
	for rows.Next() {
		it := &model.Item{}
		if err := rows.Scan(&it.ModID, &it.ItemID, &it.Name, &it.MaxStack); err != nil {
			return nil, fmt.Errorf("db: scan item produced by: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ListFluidsProducedBy is ListItemsProducedBy's fluid mirror — same (modID, machineID)
// filter shape, joining recipe_fluid_outputs instead.
func (d *DB) ListFluidsProducedBy(ctx context.Context, modID, machineID string) ([]*model.Fluid, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, fluid_id, fname FROM (
			SELECT DISTINCT f.mod_id, f.fluid_id,
			       COALESCE((SELECT t.name FROM translations t WHERE t.lang = 'en_us' AND t.lang_key IN (
			           'fluid.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'block.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'item.'  || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END
			       ) LIMIT 1), '') AS fname
			FROM fluids f
			JOIN recipe_fluid_outputs rfo ON rfo.fluid_mod_id = f.mod_id AND rfo.fluid_id = f.fluid_id
			JOIN recipes r ON r.id = rfo.recipe_id
			WHERE ( ($2 <> '' AND r.machine_mod_id = $1 AND r.machine_id = $2)
			     OR ($2 = ''  AND $1 <> '' AND r.source_mod_id = $1) )

			UNION

			SELECT DISTINCT f.mod_id, f.fluid_id,
			       COALESCE((SELECT t.name FROM translations t WHERE t.lang = 'en_us' AND t.lang_key IN (
			           'fluid.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'block.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'item.'  || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END
			       ) LIMIT 1), '') AS fname
			FROM fluids f
			JOIN recipe_fluid_outputs rfo ON rfo.fluid_mod_id = f.mod_id AND rfo.fluid_id = f.fluid_id
			JOIN recipes r ON r.id = rfo.recipe_id
			JOIN machine_interfaces mi ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
			WHERE $2 <> '' AND mi.machine_mod_id = $1 AND mi.machine_id = $2
		) sub
		ORDER BY fname
	`, modID, machineID)
	if err != nil {
		return nil, fmt.Errorf("db: list fluids produced by: %w", err)
	}
	defer rows.Close()

	var fluids []*model.Fluid
	for rows.Next() {
		f := &model.Fluid{}
		if err := rows.Scan(&f.ModID, &f.FluidID, &f.Name); err != nil {
			return nil, fmt.Errorf("db: scan fluid produced by: %w", err)
		}
		fluids = append(fluids, f)
	}
	return fluids, rows.Err()
}

// SearchItems returns up to 50 items matching query q across all mods, ordered by name.
func (d *DB) SearchItems(ctx context.Context, q string, offset int) ([]*model.Item, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT i.mod_id, i.item_id,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		           ''
		       ) AS iname,
		       i.max_stack
		FROM items i
		WHERE $1 = ''
		   OR i.item_id ILIKE '%' || $1 || '%'
		   OR EXISTS (SELECT 1 FROM translations WHERE lang='en_us'
		              AND lang_key IN ('item.'||i.mod_id||'.'||i.item_id, 'block.'||i.mod_id||'.'||i.item_id)
		              AND name ILIKE '%' || $1 || '%')
		ORDER BY COALESCE(
		    (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		    (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		    i.item_id
		), i.mod_id
		LIMIT 50 OFFSET $2
	`, q, offset)
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

// ListAllItems returns all items across all mods with translated names, ordered by mod then
// name. saveID, if non-nil, restricts results to items whose mod is active for that save —
// nil means unfiltered (catalog pages, demo mode).
func (d *DB) ListAllItems(ctx context.Context, saveID *uuid.UUID) ([]*model.Item, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT i.mod_id, i.item_id,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		           ''
		       ) AS iname,
		       i.max_stack
		FROM items i
		WHERE $1::uuid IS NULL OR i.mod_id IN (SELECT mod_id FROM save_active_mods WHERE save_id = $1)
		ORDER BY i.mod_id, COALESCE(
		    (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		    (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		    i.item_id
		)
	`, saveID)
	if err != nil {
		return nil, fmt.Errorf("db: list all items: %w", err)
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
	return items, rows.Err()
}

// ListAllFluids returns all fluids across all mods with translated names, ordered by mod
// then name. saveID, if non-nil, restricts results to fluids whose mod is active for that
// save — nil means unfiltered (catalog pages, demo mode).
func (d *DB) ListAllFluids(ctx context.Context, saveID *uuid.UUID) ([]*model.Fluid, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, fluid_id, name FROM (
			SELECT f.mod_id, f.fluid_id,
			       -- fluid_id sometimes carries its own "mod:id" namespace (e.g. a
			       -- cross-mod vanilla fluid registered under another mod's fluids:
			       -- list, like "minecraft:water" under modern_industrialization) —
			       -- look up translations under THAT namespace, not mod_id+fluid_id.
			       COALESCE((SELECT t.name FROM translations t WHERE t.lang = 'en_us' AND t.lang_key IN (
			           'fluid.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'block.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'item.'  || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END
			       ) LIMIT 1), '') AS name
			FROM fluids f
			WHERE $1::uuid IS NULL OR f.mod_id IN (SELECT mod_id FROM save_active_mods WHERE save_id = $1)
		) sub
		ORDER BY mod_id, COALESCE(NULLIF(name, ''), fluid_id)
	`, saveID)
	if err != nil {
		return nil, fmt.Errorf("db: list all fluids: %w", err)
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
	return fluids, rows.Err()
}

// ListFluidsByMod returns all fluids for modID ordered by fluid_id.
func (d *DB) ListFluidsByMod(ctx context.Context, modID string) ([]*model.Fluid, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, fluid_id, name FROM (
			SELECT f.mod_id, f.fluid_id,
			       COALESCE((SELECT t.name FROM translations t WHERE t.lang = 'en_us' AND t.lang_key IN (
			           'fluid.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'block.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'item.'  || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END
			       ) LIMIT 1), '') AS name
			FROM fluids f
			WHERE f.mod_id = $1
		) sub
		ORDER BY COALESCE(NULLIF(name, ''), fluid_id)
	`, modID)
	if err != nil {
		return nil, fmt.Errorf("db: list fluids by mod: %w", err)
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
	return fluids, rows.Err()
}

// SearchFluids returns up to 50 fluids matching query q across all mods, ordered by name.
func (d *DB) SearchFluids(ctx context.Context, q string, offset int) ([]*model.Fluid, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, fluid_id, name FROM (
			SELECT f.mod_id, f.fluid_id,
			       COALESCE((SELECT t.name FROM translations t WHERE t.lang = 'en_us' AND t.lang_key IN (
			           'fluid.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'block.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END,
			           'item.'  || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 1) ELSE f.mod_id END
			                    || '.' || CASE WHEN f.fluid_id LIKE '%:%' THEN split_part(f.fluid_id, ':', 2) ELSE f.fluid_id END
			       ) LIMIT 1), '') AS name
			FROM fluids f
		) sub
		WHERE $1 = '' OR name ILIKE '%' || $1 || '%' OR fluid_id ILIKE '%' || $1 || '%'
		ORDER BY COALESCE(NULLIF(name, ''), fluid_id), mod_id
		LIMIT 50 OFFSET $2
	`, q, offset)
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

// ListTagMembers returns every member of every item and fluid tag, ordered by kind, tag,
// mod and id.
func (d *DB) ListTagMembers(ctx context.Context) ([]model.TagMember, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT t.kind, t.name, m.item_mod_id, m.item_id
		FROM tags t JOIN tag_members m ON m.tag_id = t.id
		UNION
		SELECT t.kind, t.name, m.fluid_mod_id, m.fluid_id
		FROM tags t JOIN tag_fluid_members m ON m.tag_id = t.id
		ORDER BY 1, 2, 3, 4
	`)
	if err != nil {
		return nil, fmt.Errorf("db: list tag members: %w", err)
	}
	defer rows.Close()
	var members []model.TagMember
	for rows.Next() {
		var m model.TagMember
		if err := rows.Scan(&m.Kind, &m.TagName, &m.ModID, &m.ID); err != nil {
			return nil, fmt.Errorf("db: scan tag member: %w", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// ListAllMachines returns all machine types across all mods, ordered by mod_id then
// machine_id.
func (d *DB) ListAllMachines(ctx context.Context) ([]*model.MachineType, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mt.mod_id, mt.machine_id,
		       mt.name,
		       (SELECT COUNT(*) FROM recipes r WHERE r.machine_mod_id = mt.mod_id AND r.machine_id = mt.machine_id)
		FROM machine_types mt
		ORDER BY mt.mod_id, mt.machine_id
	`)
	if err != nil {
		return nil, fmt.Errorf("db: list all machines: %w", err)
	}
	defer rows.Close()
	var machines []*model.MachineType
	for rows.Next() {
		mt := &model.MachineType{}
		if err := rows.Scan(&mt.ModID, &mt.MachineID, &mt.Name, &mt.RecipeCount); err != nil {
			return nil, fmt.Errorf("db: scan machine: %w", err)
		}
		machines = append(machines, mt)
	}
	return machines, rows.Err()
}

// ListAllMachinesGrouped returns all machine types across all mods, with tier-variant
// machines folded into their base machine's Variants (see groupMachines).
func (d *DB) ListAllMachinesGrouped(ctx context.Context) ([]*model.MachineType, error) {
	all, err := d.ListAllMachines(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := d.Pool.Query(ctx, `SELECT machine_mod_id, machine_id, base_mod_id, base_machine_id FROM machine_interfaces`)
	if err != nil {
		return nil, fmt.Errorf("db: list machine interfaces for grouping: %w", err)
	}
	defer rows.Close()

	var interfaces []MachineInterface
	for rows.Next() {
		var mi MachineInterface
		if err := rows.Scan(&mi.MachineModID, &mi.MachineID, &mi.BaseModID, &mi.BaseMachineID); err != nil {
			return nil, fmt.Errorf("db: scan machine interface for grouping: %w", err)
		}
		interfaces = append(interfaces, mi)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groupMachines(all, interfaces), nil
}

// groupMachines folds each single-base implementer into its base's Variants (alphabetical
// by Name, RecipeCount summed). A machine with zero or more than one base edge stays its
// own top-level row — more than one means a genuinely multi-purpose machine, not a tier
// variant of anything.
func groupMachines(all []*model.MachineType, interfaces []MachineInterface) []*model.MachineType {
	type key struct{ mod, machine string }
	basesOf := map[key][]key{} // implementer -> every base it implements
	for _, mi := range interfaces {
		k := key{mi.MachineModID, mi.MachineID}
		basesOf[k] = append(basesOf[k], key{mi.BaseModID, mi.BaseMachineID})
	}
	baseOf := map[key]key{} // implementer -> its sole base, only for implementers with exactly one
	for k, bases := range basesOf {
		if len(bases) == 1 {
			baseOf[k] = bases[0]
		}
	}

	groups := map[key][]*model.MachineType{} // base key -> its implementers (base excluded here, added below)
	for _, m := range all {
		k := key{m.ModID, m.MachineID}
		if base, isImplementer := baseOf[k]; isImplementer {
			groups[base] = append(groups[base], m)
		}
	}

	var out []*model.MachineType
	for _, m := range all {
		k := key{m.ModID, m.MachineID}
		if _, isImplementer := baseOf[k]; isImplementer {
			continue // folded into its base below, not a top-level row
		}
		implementers := groups[k]
		if len(implementers) == 0 {
			out = append(out, m)
			continue
		}
		grouped := *m // copy: don't mutate the shared slice element
		members := append([]*model.MachineType{m}, implementers...)
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		grouped.RecipeCount = 0
		grouped.Variants = make([]model.MachineVariant, 0, len(members))
		for _, v := range members {
			grouped.RecipeCount += v.RecipeCount
			grouped.Variants = append(grouped.Variants, model.MachineVariant{
				ModID: v.ModID, MachineID: v.MachineID, Name: v.Name,
			})
		}
		out = append(out, &grouped)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModID != out[j].ModID {
			return out[i].ModID < out[j].ModID
		}
		return out[i].MachineID < out[j].MachineID
	})
	return out
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
		SELECT mod_id, machine_id, name
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
		if err := rows.Scan(&mt.ModID, &mt.MachineID, &mt.Name); err != nil {
			return nil, fmt.Errorf("db: scan machine: %w", err)
		}
		machines = append(machines, mt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list machines: %w", err)
	}
	return machines, nil
}

func (d *DB) ListRecipesByMod(ctx context.Context, modID, machineID string) ([]*model.Recipe, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT r.id::text, r.machine_mod_id, r.machine_id, r.source_mod_id,
		       mt.name,
		       r.name, r.duration_ticks, r.shape
		FROM recipes r
		LEFT JOIN machine_types mt ON mt.mod_id = r.machine_mod_id AND mt.machine_id = r.machine_id
		WHERE r.machine_mod_id = $1
		  AND ($2 = '' OR r.machine_id = $2)

		UNION

		SELECT r.id::text, mi.machine_mod_id, mi.machine_id, r.source_mod_id,
		       mt.name,
		       r.name, r.duration_ticks, r.shape
		FROM recipes r
		JOIN machine_interfaces mi ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
		LEFT JOIN machine_types mt ON mt.mod_id = mi.machine_mod_id AND mt.machine_id = mi.machine_id
		WHERE mi.machine_mod_id = $1
		  AND ($2 = '' OR mi.machine_id = $2)

		ORDER BY machine_id
	`, modID, machineID)
	if err != nil {
		return nil, fmt.Errorf("db: list recipes: %w", err)
	}
	defer rows.Close()

	var recipes []*model.Recipe
	for rows.Next() {
		rec := &model.Recipe{}
		if err := rows.Scan(
			&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.SourceModID, &rec.MachineName, &rec.Name,
			&rec.DurationTicks, &rec.Shape,
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

func (d *DB) hydrateRecipeIO(ctx context.Context, rec *model.Recipe) error {
	rows, err := d.Pool.Query(ctx, `
		SELECT rii.id::text, rii.recipe_id::text,
		       COALESCE(rii.item_mod_id, ''), COALESCE(rii.item_id, ''),
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||rii.item_mod_id||'.'||rii.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||rii.item_mod_id||'.'||rii.item_id),
		           ''
		       ),
		       COALESCE(rii.tag_id::text, ''), COALESCE(t.name, ''),
		       rii.amount_num, rii.amount_den, rii.probability_num, rii.probability_den,
		       rii.non_consuming
		FROM recipe_item_inputs rii
		LEFT JOIN tags t ON t.id = rii.tag_id
		WHERE rii.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		in := model.RecipeItemIO{}
		var modID, itemID, itemName, tagID, tagName string
		if err := rows.Scan(&in.ID, &in.RecipeID, &modID, &itemID, &itemName,
			&tagID, &tagName,
			&in.AmountNum, &in.AmountDen, &in.ProbabilityNum, &in.ProbabilityDen,
			&in.NonConsuming); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		in.ItemModID = strPtrOr(modID)
		in.ItemID = strPtrOr(itemID)
		in.ItemName = strPtrOr(itemName)
		in.TagID = strPtrOr(tagID)
		in.TagName = strPtrOr(tagName)
		rec.ItemInputs = append(rec.ItemInputs, in)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}

	// Item outputs.
	rows2, err := d.Pool.Query(ctx, `
		SELECT rio.id::text, rio.recipe_id::text,
		       COALESCE(rio.item_mod_id, ''), COALESCE(rio.item_id, ''),
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||rio.item_mod_id||'.'||rio.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||rio.item_mod_id||'.'||rio.item_id),
		           ''
		       ),
		       rio.amount_num, rio.amount_den, rio.probability_num, rio.probability_den
		FROM recipe_item_outputs rio
		WHERE rio.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		out := model.RecipeItemIO{}
		var modID, itemID, itemName string
		if err := rows2.Scan(&out.ID, &out.RecipeID, &modID, &itemID, &itemName,
			&out.AmountNum, &out.AmountDen, &out.ProbabilityNum, &out.ProbabilityDen); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		out.ItemModID = strPtrOr(modID)
		out.ItemID = strPtrOr(itemID)
		out.ItemName = strPtrOr(itemName)
		rec.ItemOutputs = append(rec.ItemOutputs, out)
	}
	if err := rows2.Err(); err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}

	// Fluid inputs.
	rows3, err := d.Pool.Query(ctx, `
		SELECT rfi.id::text, rfi.recipe_id::text,
		       COALESCE(rfi.fluid_mod_id, ''), COALESCE(rfi.fluid_id, ''),
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||rfi.fluid_mod_id||'.'||rfi.fluid_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||rfi.fluid_mod_id||'.'||rfi.fluid_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||rfi.fluid_mod_id||'.'||rfi.fluid_id),
		           ''
		       ),
		       t.name, rfi.amount_mb, rfi.probability_num, rfi.probability_den
		FROM recipe_fluid_inputs rfi
		LEFT JOIN tags t ON t.id = rfi.tag_id
		WHERE rfi.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		in := model.RecipeFluidIO{}
		var fluidModID, fluidID, fluidName string
		if err := rows3.Scan(&in.ID, &in.RecipeID, &fluidModID, &fluidID, &fluidName,
			&in.TagName, &in.AmountMB, &in.ProbabilityNum, &in.ProbabilityDen); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		in.FluidModID = fluidModID
		in.FluidID = fluidID
		in.FluidName = strPtrOr(fluidName)
		rec.FluidInputs = append(rec.FluidInputs, in)
	}
	if err := rows3.Err(); err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}

	// Fluid outputs.
	rows4, err := d.Pool.Query(ctx, `
		SELECT rfo.id::text, rfo.recipe_id::text, rfo.fluid_mod_id, rfo.fluid_id,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='fluid.'||rfo.fluid_mod_id||'.'||rfo.fluid_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||rfo.fluid_mod_id||'.'||rfo.fluid_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||rfo.fluid_mod_id||'.'||rfo.fluid_id),
		           ''
		       ),
		       rfo.amount_mb, rfo.probability_num, rfo.probability_den
		FROM recipe_fluid_outputs rfo
		WHERE rfo.recipe_id = $1
	`, rec.ID)
	if err != nil {
		return fmt.Errorf("db: hydrate recipe io: %w", err)
	}
	defer rows4.Close()
	for rows4.Next() {
		out := model.RecipeFluidIO{}
		var fluidName string
		if err := rows4.Scan(&out.ID, &out.RecipeID, &out.FluidModID, &out.FluidID, &fluidName,
			&out.AmountMB, &out.ProbabilityNum, &out.ProbabilityDen); err != nil {
			return fmt.Errorf("db: hydrate recipe io: %w", err)
		}
		out.FluidName = strPtrOr(fluidName)
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

// AddMachineInterface adds an "A implements B" relationship.
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

// ListMachineSlots returns all slot definitions for a machine, ordered by slot_index.
func (d *DB) ListMachineSlots(ctx context.Context, modID, machineID string) ([]*model.MachineSlot, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT slot_index, slot_type, slot_x, slot_y, label
		FROM machine_slots
		WHERE mod_id = $1 AND machine_id = $2
		ORDER BY slot_index
	`, modID, machineID)
	if err != nil {
		return nil, fmt.Errorf("db: list machine slots: %w", err)
	}
	defer rows.Close()

	var slots []*model.MachineSlot
	for rows.Next() {
		s := &model.MachineSlot{}
		if err := rows.Scan(&s.SlotIndex, &s.SlotType, &s.SlotX, &s.SlotY, &s.Label); err != nil {
			return nil, fmt.Errorf("db: scan machine slot: %w", err)
		}
		slots = append(slots, s)
	}
	return slots, rows.Err()
}

// ListVillagerTrades returns all villager trades with resolved item names. Optionally
// filtered by profession and/or tier (zero value = no filter).
func (d *DB) ListVillagerTrades(ctx context.Context, profession string, tier int) ([]*model.VillagerTradeView, error) {
	q := `
		SELECT vt.id, vt.source_mod_id,
		       COALESCE((SELECT name FROM mods WHERE mod_id = vt.source_mod_id), vt.source_mod_id) AS source_name,
		       vt.profession, vt.tier,
		       vt.cost_mod_id, vt.cost_item_id, vt.cost_count,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||vt.cost_mod_id||'.'||vt.cost_item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||vt.cost_mod_id||'.'||vt.cost_item_id),
		           vt.cost_item_id
		       ) AS cost_name,
		       vt.cost2_mod_id, vt.cost2_item_id, vt.cost2_count,
		       CASE WHEN vt.cost2_item_id IS NULL THEN NULL ELSE COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||vt.cost2_mod_id||'.'||vt.cost2_item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||vt.cost2_mod_id||'.'||vt.cost2_item_id),
		           vt.cost2_item_id
		       ) END AS cost2_name,
		       vt.result_mod_id, vt.result_item_id, vt.result_count, vt.result_modified,
		       vt.cost_variable,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||vt.result_mod_id||'.'||vt.result_item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||vt.result_mod_id||'.'||vt.result_item_id),
		           vt.result_item_id
		       ) AS result_name,
		       vt.max_uses, vt.xp
		FROM villager_trades vt
	`
	args := []any{}
	conds := []string{}
	if profession != "" {
		conds = append(conds, fmt.Sprintf("vt.profession = $%d", len(args)+1))
		args = append(args, profession)
	}
	if tier > 0 {
		conds = append(conds, fmt.Sprintf("vt.tier = $%d", len(args)+1))
		args = append(args, tier)
	}
	if len(conds) > 0 {
		q += " WHERE " + conds[0]
		for _, c := range conds[1:] {
			q += " AND " + c
		}
	}
	q += " ORDER BY vt.source_mod_id, vt.profession, vt.tier, vt.result_item_id, vt.trade_key"

	rows, err := d.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db: list villager trades: %w", err)
	}
	defer rows.Close()

	var trades []*model.VillagerTradeView
	for rows.Next() {
		t := &model.VillagerTradeView{}
		if err := rows.Scan(
			&t.ID, &t.SourceModID, &t.SourceName, &t.Profession, &t.Tier,
			&t.CostModID, &t.CostItemID, &t.CostCount, &t.CostName,
			&t.Cost2ModID, &t.Cost2ItemID, &t.Cost2Count, &t.Cost2Name,
			&t.ResultModID, &t.ResultItemID, &t.ResultCount, &t.ResultModified, &t.CostVariable, &t.ResultName,
			&t.MaxUses, &t.XP,
		); err != nil {
			return nil, fmt.Errorf("db: scan villager trade: %w", err)
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}
