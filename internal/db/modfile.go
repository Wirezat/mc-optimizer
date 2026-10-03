package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// marshalModData JSON-encodes a mod's opaque extra fields for a mod_data JSONB column,
// treating a nil/empty map as an explicit empty object.
func marshalModData(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// UpsertMod inserts or updates a mod record.
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

// UpsertFluids inserts fluid records for a mod.
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

// UpsertDirectTagMembers replaces sourceModID's members of the tag (kind, tagName) with
// members ("mod_id:id"), skipping and counting those not in the catalog.
func (d *DB) UpsertDirectTagMembers(ctx context.Context, sourceModID, kind, tagName string, members []string) (int, error) {
	table, modCol, idCol, catalog := "tag_members", "item_mod_id", "item_id", "items"
	if kind == model.TagKindFluid {
		table, modCol, idCol, catalog = "tag_fluid_members", "fluid_mod_id", "fluid_id", "fluids"
	}
	skipped := 0
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("db: upsert tag %q: begin: %w", tagName, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		DELETE FROM `+table+` m USING tags t
		WHERE t.id = m.tag_id AND t.kind = $1 AND t.name = $2 AND m.source_mod_id = $3`,
		kind, tagName, sourceModID); err != nil {
		return 0, fmt.Errorf("db: upsert tag %q: clear members: %w", tagName, err)
	}
	if len(members) > 0 {
		tagID, err := upsertTag(ctx, tx, kind, tagName)
		if err != nil {
			return 0, err
		}
		for _, ref := range members {
			modID, id, err := splitColonRef(ref)
			if err != nil {
				return 0, fmt.Errorf("db: tag member %q: %w", ref, err)
			}
			res, err := tx.Exec(ctx, `
				INSERT INTO `+table+` (tag_id, `+modCol+`, `+idCol+`, source_mod_id)
				SELECT $1, $2, $3, $4
				WHERE EXISTS (SELECT 1 FROM `+catalog+` c WHERE c.mod_id = $2 AND c.`+idCol+` = $3)
				ON CONFLICT DO NOTHING`,
				tagID, modID, id, sourceModID)
			if err != nil {
				return 0, fmt.Errorf("db: upsert tag member %s: %w", ref, err)
			}
			if res.RowsAffected() == 0 {
				skipped++
			}
		}
	}
	return skipped, tx.Commit(ctx)
}

func splitColonRef(ref string) (modID, id string, err error) {
	for i, c := range ref {
		if c == ':' {
			return ref[:i], ref[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid ref %q: expected mod_id:id", ref)
}
