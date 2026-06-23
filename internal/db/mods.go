package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
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

// LookupMachineNames resolves en_us display names for a set of (modID, machineID) pairs.
// Uses name_lang_key for i18n lookup, falls back to name (titlecase).
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
		       COALESCE(
		           (SELECT t.name FROM translations t WHERE t.lang = 'en_us' AND t.lang_key = mt.name_lang_key),
		           mt.name
		       )
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

// ListMods returns all mods ordered by name, including optional Modrinth metadata.
func (d *DB) ListMods(ctx context.Context) ([]*model.Mod, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mod_id, name, energy_type,
		       description, author, license,
		       url_source, url_modrinth, url_wiki, url_issues, url_discord,
		       modrinth_slug
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
		if err := rows.Scan(
			&m.ModID, &m.Name, &m.EnergyType,
			&m.Description, &m.Author, &m.License,
			&m.URLSource, &m.URLModrinth, &m.URLWiki, &m.URLIssues, &m.URLDiscord,
			&m.ModrinthSlug,
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

// UpdateModFull overwrites all editable fields for a mod. Caller must be an admin.
// Unlike UpdateModMetadata, this always overwrites (no COALESCE) — used for manual admin edits.
func (d *DB) UpdateModFull(ctx context.Context, modID string, u model.ModUpdate) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE mods SET
			name          = COALESCE($2,  name),
			energy_type   = COALESCE($3,  energy_type),
			description   = $4,
			author        = $5,
			license       = $6,
			url_source    = $7,
			url_modrinth  = $8,
			url_wiki      = $9,
			url_issues    = $10,
			url_discord   = $11,
			modrinth_slug = $12
		WHERE mod_id = $1
	`, modID, u.Name, u.EnergyType,
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

// UpdateMachineType updates fields of a machine type. Caller must be an admin (enforced at API layer).
func (d *DB) UpdateMachineType(ctx context.Context, modID, machineID string, name *string, baseEUPerTick, maxEUPerTick *int64) error {
	tag, err := d.Pool.Exec(ctx, `
		UPDATE machine_types
		SET
			name             = COALESCE($3, name),
			base_eu_per_tick = COALESCE($4, base_eu_per_tick),
			max_eu_per_tick  = COALESCE($5, max_eu_per_tick)
		WHERE mod_id = $1 AND machine_id = $2
	`, modID, machineID, name, baseEUPerTick, maxEUPerTick)
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

// UpdateModMetadata stores Modrinth-fetched metadata for a mod.
// Only updates fields that are currently NULL, so manually set values are preserved.
func (d *DB) UpdateModMetadata(ctx context.Context, meta model.ModMetadata) error {
	_, err := d.Pool.Exec(ctx, `
		UPDATE mods SET
			description   = COALESCE(description,   NULLIF($2, '')),
			author        = COALESCE(author,         NULLIF($3, '')),
			license       = COALESCE(license,        NULLIF($4, '')),
			url_source    = COALESCE(url_source,     NULLIF($5, '')),
			url_modrinth  = COALESCE(url_modrinth,   NULLIF($6, '')),
			url_wiki      = COALESCE(url_wiki,       NULLIF($7, '')),
			url_issues    = COALESCE(url_issues,     NULLIF($8, '')),
			url_discord   = COALESCE(url_discord,    NULLIF($9, '')),
			modrinth_slug = COALESCE(modrinth_slug,  NULLIF($10, ''))
		WHERE mod_id = $1
	`, meta.ModID, meta.Description, meta.Author, meta.License,
		meta.URLSource, meta.URLModrinth, meta.URLWiki, meta.URLIssues,
		meta.URLDiscord, meta.ModrinthSlug)
	if err != nil {
		return fmt.Errorf("db: update mod metadata %s: %w", meta.ModID, err)
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
// Used by catalog pages; no pagination limit.
func (d *DB) ListAllItems(ctx context.Context) ([]*model.Item, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT i.mod_id, i.item_id,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		           ''
		       ) AS iname,
		       i.max_stack
		FROM items i
		ORDER BY i.mod_id, COALESCE(
		    (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||i.mod_id||'.'||i.item_id),
		    (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||i.mod_id||'.'||i.item_id),
		    i.item_id
		)
	`)
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
func (d *DB) ListAllFluids(ctx context.Context) ([]*model.Fluid, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT f.mod_id, f.fluid_id, COALESCE(t.name, '')
		FROM fluids f
		LEFT JOIN translations t ON t.lang = 'en_us' AND t.lang_key = 'fluid.' || f.mod_id || '.' || f.fluid_id
		ORDER BY f.mod_id, COALESCE(t.name, f.fluid_id)
	`)
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
		SELECT f.mod_id, f.fluid_id, COALESCE(t.name, '')
		FROM fluids f
		LEFT JOIN translations t ON t.lang = 'en_us' AND t.lang_key = 'fluid.' || f.mod_id || '.' || f.fluid_id
		WHERE f.mod_id = $1
		ORDER BY COALESCE(t.name, f.fluid_id)
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
		SELECT f.mod_id, f.fluid_id, COALESCE(t.name, '')
		FROM fluids f
		LEFT JOIN translations t ON t.lang = 'en_us' AND t.lang_key = 'fluid.' || f.mod_id || '.' || f.fluid_id
		WHERE $1 = '' OR t.name ILIKE '%' || $1 || '%' OR f.fluid_id ILIKE '%' || $1 || '%'
		ORDER BY COALESCE(t.name, f.fluid_id), f.mod_id
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

// ListAllMachines returns all machine types across all mods, ordered by mod_id then machine_id.
func (d *DB) ListAllMachines(ctx context.Context) ([]*model.MachineType, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mt.mod_id, mt.machine_id,
		       COALESCE((SELECT t.name FROM translations t WHERE t.lang='en_us' AND t.lang_key = mt.name_lang_key), mt.name),
		       COALESCE(mt.base_eu_per_tick, 0), COALESCE(mt.max_eu_per_tick, 0),
		       COALESCE(mt.max_slots, 0), COALESCE(mt.energy_type, ''),
		       COALESCE(mt.upgradable, false),
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
		if err := rows.Scan(
			&mt.ModID, &mt.MachineID, &mt.Name,
			&mt.BaseEUPerTick, &mt.MaxEUPerTick, &mt.MaxSlots,
			&mt.EnergyType, &mt.Upgradable, &mt.RecipeCount,
		); err != nil {
			return nil, fmt.Errorf("db: scan machine: %w", err)
		}
		machines = append(machines, mt)
	}
	return machines, rows.Err()
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
// ListRecipesCatalog returns all recipes with optional mod/machine filter, without IO hydration.
// Used by the catalog page; IO details are fetched on expand.
func (d *DB) ListRecipesCatalog(ctx context.Context, modID, machineID string) ([]*model.Recipe, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT r.id::text, r.machine_mod_id, r.machine_id,
		       COALESCE((SELECT t.name FROM translations t WHERE t.lang='en_us' AND t.lang_key = mt.name_lang_key), mt.name),
		       r.name,
		       r.duration_ticks, r.eu_per_tick, r.total_eu
		FROM recipes r
		LEFT JOIN machine_types mt ON mt.mod_id = r.machine_mod_id AND mt.machine_id = r.machine_id
		WHERE ($1 = '' OR r.machine_mod_id = $1)
		  AND ($2 = '' OR r.machine_id = $2)
		ORDER BY r.machine_mod_id, r.machine_id
	`, modID, machineID)
	if err != nil {
		return nil, fmt.Errorf("db: list recipes catalog: %w", err)
	}
	defer rows.Close()
	var recipes []*model.Recipe
	for rows.Next() {
		rec := &model.Recipe{}
		if err := rows.Scan(
			&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.MachineName, &rec.Name,
			&rec.DurationTicks, &rec.EUPerTick, &rec.TotalEU,
		); err != nil {
			return nil, fmt.Errorf("db: scan recipe: %w", err)
		}
		recipes = append(recipes, rec)
	}
	return recipes, rows.Err()
}

func (d *DB) ListRecipesByMod(ctx context.Context, modID, machineID string) ([]*model.Recipe, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT r.id::text, r.machine_mod_id, r.machine_id,
		       COALESCE((SELECT t.name FROM translations t WHERE t.lang='en_us' AND t.lang_key = mt.name_lang_key), mt.name),
		       r.name, r.duration_ticks, r.eu_per_tick, r.total_eu, r.shape
		FROM recipes r
		LEFT JOIN machine_types mt ON mt.mod_id = r.machine_mod_id AND mt.machine_id = r.machine_id
		WHERE r.machine_mod_id = $1
		  AND ($2 = '' OR r.machine_id = $2)

		UNION

		SELECT r.id::text, mi.machine_mod_id, mi.machine_id,
		       COALESCE((SELECT t.name FROM translations t WHERE t.lang='en_us' AND t.lang_key = mt.name_lang_key), mt.name),
		       r.name, r.duration_ticks, r.eu_per_tick, r.total_eu, r.shape
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
			&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.MachineName, &rec.Name,
			&rec.DurationTicks, &rec.EUPerTick, &rec.TotalEU, &rec.Shape,
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
		INSERT INTO recipes (id, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
		RETURNING id::text, machine_mod_id, machine_id, duration_ticks, eu_per_tick, total_eu
	`, modID, req.MachineID, req.DurationTicks, req.EUPerTick, int64(req.DurationTicks)*req.EUPerTick,
	).Scan(&rec.ID, &rec.MachineModID, &rec.MachineID, &rec.DurationTicks, &rec.EUPerTick, &rec.TotalEU); err != nil {
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
		       COALESCE(t.name, ''),
		       rfi.amount_mb, rfi.probability_num, rfi.probability_den
		FROM recipe_fluid_inputs rfi
		LEFT JOIN translations t ON t.lang = 'en_us'
		                        AND t.lang_key = 'fluid.' || rfi.fluid_mod_id || '.' || rfi.fluid_id
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
		       COALESCE(t.name, ''),
		       rfo.amount_mb, rfo.probability_num, rfo.probability_den
		FROM recipe_fluid_outputs rfo
		LEFT JOIN translations t ON t.lang = 'en_us'
		                        AND t.lang_key = 'fluid.' || rfo.fluid_mod_id || '.' || rfo.fluid_id
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
		SELECT vt.id, vt.profession, vt.tier,
		       vt.cost_mod_id, vt.cost_item_id, vt.cost_count,
		       COALESCE(
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='item.'||vt.cost_mod_id||'.'||vt.cost_item_id),
		           (SELECT name FROM translations WHERE lang='en_us' AND lang_key='block.'||vt.cost_mod_id||'.'||vt.cost_item_id),
		           vt.cost_item_id
		       ) AS cost_name,
		       vt.result_mod_id, vt.result_item_id, vt.result_count, vt.result_modified,
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
	q += " ORDER BY vt.profession, vt.tier, vt.result_item_id"

	rows, err := d.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db: list villager trades: %w", err)
	}
	defer rows.Close()

	var trades []*model.VillagerTradeView
	for rows.Next() {
		t := &model.VillagerTradeView{}
		if err := rows.Scan(
			&t.ID, &t.Profession, &t.Tier,
			&t.CostModID, &t.CostItemID, &t.CostCount, &t.CostName,
			&t.ResultModID, &t.ResultItemID, &t.ResultCount, &t.ResultModified, &t.ResultName,
			&t.MaxUses, &t.XP,
		); err != nil {
			return nil, fmt.Errorf("db: scan villager trade: %w", err)
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}

// ListUpgradeTiers returns all upgrade tiers across mods, cheapest (lowest EU bonus) first.
// Used by the solve UI to populate the upgrade tier picker.
func (d *DB) ListUpgradeTiers(ctx context.Context) ([]model.UpgradeTier, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, mod_id, name, COALESCE(eu_bonus_per_slot, 0), item_ref
		FROM upgrade_tiers
		ORDER BY eu_bonus_per_slot NULLS LAST, name
	`)
	if err != nil {
		return nil, fmt.Errorf("db: list upgrade tiers: %w", err)
	}
	defer rows.Close()

	var tiers []model.UpgradeTier
	for rows.Next() {
		var t model.UpgradeTier
		if err := rows.Scan(&t.ID, &t.ModID, &t.Name, &t.EUBonusPerSlot, &t.ItemID); err != nil {
			return nil, fmt.Errorf("db: scan upgrade tier: %w", err)
		}
		tiers = append(tiers, t)
	}
	return tiers, rows.Err()
}
