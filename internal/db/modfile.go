package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// marshalModData JSON-encodes a mod's opaque extra fields for a mod_data
// JSONB column, treating a nil/empty map as an explicit empty object.
func marshalModData(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// UpsertMod inserts or updates a mod record. Fields that are empty strings are
// not overwritten (DO UPDATE only touches non-empty values).
func (d *DB) UpsertMod(ctx context.Context, m model.ModDef) error {
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO mods (mod_id, name, description, author, license,
		                  modrinth_slug, url_source, url_modrinth, url_wiki, url_issues, url_discord)
		VALUES ($1, $2,
		        NULLIF($3,''), NULLIF($4,''), NULLIF($5,''),
		        NULLIF($6,''), NULLIF($7,''), NULLIF($8,''),
		        NULLIF($9,''), NULLIF($10,''), NULLIF($11,''))
		ON CONFLICT (mod_id) DO UPDATE SET
		    name          = CASE WHEN EXCLUDED.name <> mods.mod_id THEN EXCLUDED.name ELSE mods.name END,
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
		m.ModID, m.Name,
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
		INSERT INTO mods (mod_id, name) VALUES ($1, $1)
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
	modData, err := marshalModData(m.ModData)
	if err != nil {
		return fmt.Errorf("db: upsert machine_type %s:%s: encode mod_data: %w", m.ModID, m.MachineID, err)
	}
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO machine_types (mod_id, machine_id, name, ecosystem, mod_data)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		ON CONFLICT (mod_id, machine_id) DO UPDATE SET
		    name      = EXCLUDED.name,
		    ecosystem = EXCLUDED.ecosystem,
		    mod_data  = EXCLUDED.mod_data
	`, m.ModID, m.MachineID, m.Name, m.Ecosystem, modData); err != nil {
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

// UpsertDirectTagMembers replaces a tag's members with the given list (already
// resolved mod_id:item_id pairs). Replacing rather than adding is what makes a
// reimport that drops a member actually drop it; the whole tag is rewritten in
// one transaction so a failing member leaves the previous membership intact.
func (d *DB) UpsertDirectTagMembers(ctx context.Context, tagName string, members []string) error {
	if len(members) == 0 {
		return nil
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: upsert tag %q: begin: %w", tagName, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var tagID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO tags (id, name)
		VALUES (gen_random_uuid(), $1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, tagName).Scan(&tagID); err != nil {
		return fmt.Errorf("db: upsert tag %q: %w", tagName, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tag_members WHERE tag_id = $1`, tagID); err != nil {
		return fmt.Errorf("db: upsert tag %q: clear members: %w", tagName, err)
	}
	for _, ref := range members {
		modID, itemID, err := splitColonRef(ref)
		if err != nil {
			return fmt.Errorf("db: tag member %q: %w", ref, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO tag_members (tag_id, item_mod_id, item_id)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, tagID, modID, itemID); err != nil {
			return fmt.Errorf("db: upsert tag member %s: %w", ref, err)
		}
	}
	return tx.Commit(ctx)
}

func splitColonRef(ref string) (modID, id string, err error) {
	for i, c := range ref {
		if c == ':' {
			return ref[:i], ref[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid ref %q: expected mod_id:id", ref)
}
