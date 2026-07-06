package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// UpsertMod inserts or updates a mod record. Fields that are empty strings are
// not overwritten (DO UPDATE only touches non-empty values).
func (d *DB) UpsertMod(ctx context.Context, m model.ModDef) error {
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO mods (mod_id, name, energy_type, description, author, license,
		                  modrinth_slug, url_source, url_modrinth, url_wiki, url_issues, url_discord)
		VALUES ($1, $2, $3,
		        NULLIF($4,''), NULLIF($5,''), NULLIF($6,''),
		        NULLIF($7,''), NULLIF($8,''), NULLIF($9,''),
		        NULLIF($10,''), NULLIF($11,''), NULLIF($12,''))
		ON CONFLICT (mod_id) DO UPDATE SET
		    name          = CASE WHEN EXCLUDED.name <> mods.mod_id THEN EXCLUDED.name ELSE mods.name END,
		    energy_type   = EXCLUDED.energy_type,
		    description   = COALESCE(NULLIF(EXCLUDED.description,''),  mods.description),
		    author        = COALESCE(NULLIF(EXCLUDED.author,''),        mods.author),
		    license       = COALESCE(NULLIF(EXCLUDED.license,''),       mods.license),
		    modrinth_slug = COALESCE(NULLIF(EXCLUDED.modrinth_slug,''), mods.modrinth_slug),
		    url_source    = COALESCE(NULLIF(EXCLUDED.url_source,''),    mods.url_source),
		    url_modrinth  = COALESCE(NULLIF(EXCLUDED.url_modrinth,''),  mods.url_modrinth),
		    url_wiki      = COALESCE(NULLIF(EXCLUDED.url_wiki,''),      mods.url_wiki),
		    url_issues    = COALESCE(NULLIF(EXCLUDED.url_issues,''),    mods.url_issues),
		    url_discord   = COALESCE(NULLIF(EXCLUDED.url_discord,''),   mods.url_discord)
	`,
		m.ModID, m.Name, m.EnergyType,
		m.Description, m.Author, m.License,
		m.ModrinthSlug, m.URLSource, m.URLModrinth,
		m.URLWiki, m.URLIssues, m.URLDiscord,
	)
	return err
}

// UpsertFluids inserts fluid records for a mod. Existing fluids are left unchanged.
func (d *DB) UpsertFluids(ctx context.Context, modID string, fluidIDs []string) error {
	if len(fluidIDs) == 0 {
		return nil
	}
	// Ensure mod exists before inserting fluids.
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO mods (mod_id, name, energy_type) VALUES ($1, $1, 'NONE')
		ON CONFLICT (mod_id) DO NOTHING
	`, modID); err != nil {
		return fmt.Errorf("db: upsert fluids: ensure mod %s: %w", modID, err)
	}
	for _, fid := range fluidIDs {
		if _, err := d.Pool.Exec(ctx, `
			INSERT INTO fluids (mod_id, fluid_id) VALUES ($1, $2)
			ON CONFLICT (mod_id, fluid_id) DO NOTHING
		`, modID, fid); err != nil {
			return fmt.Errorf("db: upsert fluid %s:%s: %w", modID, fid, err)
		}
	}
	return nil
}

// UpsertMachineType inserts or updates a machine_type record and its slots.
func (d *DB) UpsertMachineType(ctx context.Context, m model.MachineTypeDef) error {
	energyType := (*string)(nil)
	if m.EnergyType != nil {
		energyType = m.EnergyType
	}
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO machine_types
		    (mod_id, machine_id, name, base_eu_per_tick, max_eu_per_tick, max_slots, energy_type, upgradable, fixed_recipe_eu_cap)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (mod_id, machine_id) DO UPDATE SET
		    name                = EXCLUDED.name,
		    base_eu_per_tick    = COALESCE(EXCLUDED.base_eu_per_tick, machine_types.base_eu_per_tick),
		    max_eu_per_tick     = COALESCE(EXCLUDED.max_eu_per_tick,  machine_types.max_eu_per_tick),
		    max_slots           = COALESCE(EXCLUDED.max_slots,        machine_types.max_slots),
		    energy_type         = COALESCE(EXCLUDED.energy_type,      machine_types.energy_type),
		    upgradable          = EXCLUDED.upgradable,
		    fixed_recipe_eu_cap = EXCLUDED.fixed_recipe_eu_cap
	`,
		m.ModID, m.MachineID, m.Name,
		m.BaseEnergyPerTick, m.MaxEnergyPerTick, m.MaxSlots,
		energyType, m.Upgradable, m.FixedRecipeEUCap,
	); err != nil {
		return fmt.Errorf("db: upsert machine_type %s:%s: %w", m.ModID, m.MachineID, err)
	}
	return d.UpsertMachineSlots(ctx, m.Slots)
}

// UpsertMachineSlots inserts or replaces all slots for a machine.
func (d *DB) UpsertMachineSlots(ctx context.Context, slots []model.MachineSlotDef) error {
	for _, s := range slots {
		if _, err := d.Pool.Exec(ctx, `
			INSERT INTO machine_slots (mod_id, machine_id, slot_index, slot_type, slot_x, slot_y, label)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (mod_id, machine_id, slot_index) DO UPDATE SET
			    slot_type = EXCLUDED.slot_type,
			    slot_x    = EXCLUDED.slot_x,
			    slot_y    = EXCLUDED.slot_y,
			    label     = EXCLUDED.label
		`, s.ModID, s.MachineID, s.Index, s.SlotType, s.X, s.Y, s.Label); err != nil {
			return fmt.Errorf("db: upsert slot %s:%s[%d]: %w", s.ModID, s.MachineID, s.Index, err)
		}
	}
	return nil
}

// UpsertDirectTagMembers inserts explicit tag members (already resolved mod_id:item_id pairs).
func (d *DB) UpsertDirectTagMembers(ctx context.Context, tagName string, members []string) error {
	if len(members) == 0 {
		return nil
	}
	var tagID string
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO tags (id, name)
		VALUES (gen_random_uuid(), $1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, tagName).Scan(&tagID); err != nil {
		return fmt.Errorf("db: upsert tag %q: %w", tagName, err)
	}
	for _, ref := range members {
		modID, itemID, err := splitColonRef(ref)
		if err != nil {
			return fmt.Errorf("db: tag member %q: %w", ref, err)
		}
		if _, err := d.Pool.Exec(ctx, `
			INSERT INTO tag_members (tag_id, item_mod_id, item_id)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, tagID, modID, itemID); err != nil {
			return fmt.Errorf("db: upsert tag member %s: %w", ref, err)
		}
	}
	return nil
}

// UpsertUpgradeTier inserts or updates an upgrade tier definition (e.g. MI's
// basic/advanced/turbo/highly_advanced/quantum upgrade items with their
// extraMaxEu bonus per slot). The item's own max_stack (items table) is the
// upgrade count cap — not stored here, see GetUpgradeTiers.
func (d *DB) UpsertUpgradeTier(ctx context.Context, modID, name string, euBonusPerSlot int64, itemRef string) error {
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO upgrade_tiers (id, mod_id, name, eu_bonus_per_slot, item_ref)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		ON CONFLICT (item_ref) DO UPDATE SET
		    mod_id            = EXCLUDED.mod_id,
		    name              = EXCLUDED.name,
		    eu_bonus_per_slot = EXCLUDED.eu_bonus_per_slot
	`, modID, name, euBonusPerSlot, itemRef); err != nil {
		return fmt.Errorf("db: upsert upgrade tier %q: %w", itemRef, err)
	}
	return nil
}

func splitColonRef(ref string) (modID, id string, err error) {
	for i, c := range ref {
		if c == ':' {
			return ref[:i], ref[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid ref %q: expected mod_id:id", ref)
}
