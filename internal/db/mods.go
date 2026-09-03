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
// Returns a map[itemKey → name]; items without a translation are omitted.
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

// LookupMachineNames resolves display names for a set of (modID, machineID) pairs.
// The name is resolved from the modfile's lang_key at import time and stored on the row.
// Returns a map[modID+":"+machineID → name].
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

// LookupMachinePluginMods resolves, for a set of (modID, machineID) pairs, the
// mod whose plugin evaluates that machine: its ecosystem, or its own mod when
// no ecosystem is set. The SQL mirrors solver.PluginMod, so the browser and
// the solver agree on which plugin owns a machine.
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
	`, modIDs, machineIDs)
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
		       (SELECT COUNT(*) FROM recipes r WHERE r.source_mod_id = m.mod_id) AS recipe_count
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
			&m.ModrinthSlug, &m.RecipeCount,
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

// CreateMod inserts a new mod. Caller must be an admin (enforced at API layer).
func (d *DB) CreateMod(ctx context.Context, modID, name string) (*model.Mod, error) {
	m := &model.Mod{}
	err := d.Pool.QueryRow(ctx, `
		INSERT INTO mods (mod_id, name)
		VALUES ($1, $2)
		RETURNING mod_id, name
	`, modID, name).Scan(&m.ModID, &m.Name)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create mod: %w", err)
	}
	return m, nil
}

// UpdateModFull overwrites all editable fields for a mod. Caller must be an admin.
// Every field but name is overwritten unconditionally, including to NULL; name falls
// back to its current value via COALESCE when nil.
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

// UpdateMachineType updates the display name of a machine type. Caller must be an admin (enforced at API layer).
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

// UpdateItem upserts the en_us translation for an item. Caller must be an admin (enforced at API layer).
func (d *DB) UpdateItem(ctx context.Context, modID, itemID, name string) error {
	var exists bool
	if err := d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE mod_id=$1 AND item_id=$2)`, modID, itemID).Scan(&exists); err != nil {
		return fmt.Errorf("db: update item: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO translations (lang, lang_key, name)
		VALUES ('en_us', 'item.' || $1 || '.' || $2, $3)
		ON CONFLICT (lang, lang_key) DO UPDATE SET name = $3
	`, modID, itemID, name)
	if err != nil {
		return fmt.Errorf("db: update item: %w", err)
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

// ListItemsProducedBy returns every item that appears as an output of a
// recipe matching (modID, machineID), same filter shape as
// ListRecipesCatalog: machineID set narrows to that exact machine (plus
// anything that implements it via machine_interfaces); machineID empty
// narrows to recipes the mod itself added (source_mod_id); both empty
// returns nothing (callers only call this with at least one set).
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

// ListFluidsProducedBy is ListItemsProducedBy's fluid mirror — same
// (modID, machineID) filter shape, joining recipe_fluid_outputs instead.
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
// offset is used for pagination (ring-buffer / infinite scroll on the frontend).
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

// ListAllItems returns all items across all mods with translated names, ordered by mod then name.
// Used by catalog pages (no pagination limit) and the Solve target-item picker.
// saveID, if non-nil, restricts results to items whose mod is active for that save —
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

// ListAllFluids returns all fluids across all mods with translated names, ordered by mod then name.
// saveID, if non-nil, restricts results to fluids whose mod is active for that save —
// nil means unfiltered (catalog pages, demo mode).
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
// offset is used for pagination (ring-buffer / infinite scroll on the frontend).
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

// SearchTags returns up to 50 tag names matching query q, ordered alphabetically.
func (d *DB) SearchTags(ctx context.Context, q string, offset int) ([]string, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT name FROM tags
		WHERE $1 = '' OR name ILIKE '%' || $1 || '%'
		ORDER BY name
		LIMIT 50 OFFSET $2
	`, q, offset)
	if err != nil {
		return nil, fmt.Errorf("db: search tags: %w", err)
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("db: scan tag: %w", err)
		}
		tags = append(tags, name)
	}
	return tags, rows.Err()
}

// ListTagMembers returns every item each tag stands for, ordered so a tag's
// members always come back in the same sequence — the UI cycles through them,
// and a cycle that reshuffled per request would be unreadable.
func (d *DB) ListTagMembers(ctx context.Context) ([]model.TagMember, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT t.name, m.item_mod_id, m.item_id
		FROM tags t
		JOIN tag_members m ON m.tag_id = t.id
		ORDER BY t.name, m.item_mod_id, m.item_id
	`)
	if err != nil {
		return nil, fmt.Errorf("db: list tag members: %w", err)
	}
	defer rows.Close()
	var members []model.TagMember
	for rows.Next() {
		var m model.TagMember
		if err := rows.Scan(&m.TagName, &m.ModID, &m.ItemID); err != nil {
			return nil, fmt.Errorf("db: scan tag member: %w", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// ListAllMachines returns all machine types across all mods, ordered by mod_id then machine_id.
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

// ListAllMachinesGrouped returns all machine types across all mods, with
// tier-variant machines folded into their base machine's Variants (see
// groupMachines). TextureURL is left nil; callers attach it via
// assets.ResolveMachineTexture, same as attachItemTextures/attachFluidTextures
// do for items/fluids.
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

// groupMachines folds each single-base implementer into its base's Variants
// (alphabetical by Name, RecipeCount summed). A machine with zero or more
// than one base edge stays its own top-level row — more than one means a
// genuinely multi-purpose machine, not a tier variant of anything. Output is
// sorted by ModID then MachineID.
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
// ListRecipesCatalog returns all recipes with optional mod/machine filter, without IO hydration.
// Used by the catalog page; IO details are fetched on expand.
// ListRecipesCatalog lists recipes for the catalog page, optionally filtered by
// machine mod/id and/or by an item or fluid that must appear among the recipe's
// OUTPUTS (not inputs) — "how is this item/fluid produced", matching the
// items/fluids/trades catalog pages' "name → recipes producing this" link convention.
//
// modID alone filters by source_mod_id; with machineID also set, modID+machineID
// identify a specific machine (machine_mod_id+machine_id) instead.
func (d *DB) ListRecipesCatalog(ctx context.Context, modID, machineID, itemModID, itemID, fluidModID, fluidID string) ([]*model.Recipe, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, machine_mod_id, machine_id, source_mod_id, machine_name, name,
		       duration_ticks
		FROM (
			SELECT r.id::text AS id, r.machine_mod_id, r.machine_id, r.source_mod_id,
			       mt.name AS machine_name,
			       r.name,
			       r.duration_ticks
			FROM recipes r
			LEFT JOIN machine_types mt ON mt.mod_id = r.machine_mod_id AND mt.machine_id = r.machine_id
			WHERE ( ($2 <> '' AND r.machine_mod_id = $1 AND r.machine_id = $2)
			     OR ($2 = ''  AND ($1 = '' OR r.source_mod_id = $1)) )
			  AND ($3 = '' OR EXISTS (
			        SELECT 1 FROM recipe_item_outputs rio
			        WHERE rio.recipe_id = r.id AND rio.item_mod_id = $3 AND rio.item_id = $4
			      ))
			  AND ($5 = '' OR EXISTS (
			        SELECT 1 FROM recipe_fluid_outputs rfo
			        WHERE rfo.recipe_id = r.id AND rfo.fluid_mod_id = $5 AND rfo.fluid_id = $6
			      ))

			UNION

			-- Interface-inherited recipes: only surfaced when a specific machine
			-- is picked (mod-level browsing keeps showing pure attribution, per
			-- the "attribute recipes to the mod that added them" fix — this is
			-- purely additive for the machine drill-down view).
			SELECT r.id::text AS id, mi.machine_mod_id, mi.machine_id, r.source_mod_id,
			       mt.name AS machine_name,
			       r.name,
			       r.duration_ticks
			FROM recipes r
			JOIN machine_interfaces mi ON mi.base_mod_id = r.machine_mod_id AND mi.base_machine_id = r.machine_id
			LEFT JOIN machine_types mt ON mt.mod_id = mi.machine_mod_id AND mt.machine_id = mi.machine_id
			WHERE $2 <> '' AND mi.machine_mod_id = $1 AND mi.machine_id = $2
			  AND ($3 = '' OR EXISTS (
			        SELECT 1 FROM recipe_item_outputs rio
			        WHERE rio.recipe_id = r.id AND rio.item_mod_id = $3 AND rio.item_id = $4
			      ))
			  AND ($5 = '' OR EXISTS (
			        SELECT 1 FROM recipe_fluid_outputs rfo
			        WHERE rfo.recipe_id = r.id AND rfo.fluid_mod_id = $5 AND rfo.fluid_id = $6
			      ))
		) sub
		ORDER BY source_mod_id, machine_mod_id, machine_id
	`, modID, machineID, itemModID, itemID, fluidModID, fluidID)
	if err != nil {
		return nil, fmt.Errorf("db: list recipes catalog: %w", err)
	}
	defer rows.Close()
	var recipes []*model.Recipe
	for rows.Next() {
		rec := &model.Recipe{}
		if err := rows.Scan(
			&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.SourceModID, &rec.MachineName, &rec.Name,
			&rec.DurationTicks,
		); err != nil {
			return nil, fmt.Errorf("db: scan recipe: %w", err)
		}
		recipes = append(recipes, rec)
	}
	return recipes, rows.Err()
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
		INSERT INTO recipes (id, machine_mod_id, machine_id, source_mod_id, duration_ticks)
		VALUES (gen_random_uuid(), $1, $2, $1, $3)
		RETURNING id::text, machine_mod_id, machine_id, source_mod_id, duration_ticks
	`, modID, req.MachineID, req.DurationTicks,
	).Scan(&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.SourceModID, &rec.DurationTicks); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("db: create recipe: %w", err)
	}

	recipeID := rec.ID

	// 2. Insert item inputs.
	for i, in := range req.ItemInputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_inputs
				(id, recipe_id, sort_index, item_mod_id, item_id, tag_id, amount_num, amount_den, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, recipeID, i, in.ItemModID, in.ItemID, in.TagID,
			in.AmountNum, in.AmountDen, in.ProbabilityNum, in.ProbabilityDen,
		); err != nil {
			return nil, fmt.Errorf("db: create recipe: %w", err)
		}
	}

	// 3. Insert item outputs.
	for i, out := range req.ItemOutputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_item_outputs
				(id, recipe_id, sort_index, item_mod_id, item_id, amount_num, amount_den, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		`, recipeID, i, out.ItemModID, out.ItemID,
			out.AmountNum, out.AmountDen, out.ProbabilityNum, out.ProbabilityDen,
		); err != nil {
			return nil, fmt.Errorf("db: create recipe: %w", err)
		}
	}

	// 4. Insert fluid inputs.
	for i, in := range req.FluidInputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_inputs
				(id, recipe_id, sort_index, fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		`, recipeID, i, in.FluidModID, in.FluidID,
			in.AmountMB, in.ProbabilityNum, in.ProbabilityDen,
		); err != nil {
			return nil, fmt.Errorf("db: create recipe: %w", err)
		}
	}

	// 5. Insert fluid outputs.
	for i, out := range req.FluidOutputs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_fluid_outputs
				(id, recipe_id, sort_index, fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		`, recipeID, i, out.FluidModID, out.FluidID,
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
	// Item inputs — COALESCE all nullable text columns to '' to avoid NULL scan issues.
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
		       rfi.amount_mb, rfi.probability_num, rfi.probability_den
		FROM recipe_fluid_inputs rfi
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
			&in.AmountMB, &in.ProbabilityNum, &in.ProbabilityDen); err != nil {
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

// ListVillagerTrades returns all villager trades with resolved item names.
// Optionally filtered by profession and/or tier (zero value = no filter).
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
