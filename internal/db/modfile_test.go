package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/google/uuid"
)

func strp(s string) *string { return &s }

// UpsertMachineType must persist both ecosystem and mod_data, not just the name — a plugin
// reads mod_data to know how the machine behaves, and ecosystem to know which mod's plugin
// evaluates it.
func TestUpsertMachineType_StoresEcosystemAndModData(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")

	m := model.MachineTypeDef{
		ModID:     "testmod",
		MachineID: "iron_furnace",
		Name:      "Iron Furnace",
		Ecosystem: "other_mod",
		ModData:   map[string]any{"tier": "steam", "fuel_slots": float64(2)},
	}
	if err := d.UpsertMachineType(ctx, m); err != nil {
		t.Fatalf("upsert machine type: %v", err)
	}

	var ecosystem string
	var modData []byte
	if err := d.Pool.QueryRow(ctx,
		`SELECT COALESCE(ecosystem, ''), mod_data FROM machine_types WHERE mod_id = $1 AND machine_id = $2`,
		"testmod", "iron_furnace").Scan(&ecosystem, &modData); err != nil {
		t.Fatalf("select: %v", err)
	}
	if ecosystem != "other_mod" {
		t.Errorf("ecosystem = %q, want %q", ecosystem, "other_mod")
	}
	var got map[string]any
	if err := json.Unmarshal(modData, &got); err != nil {
		t.Fatalf("unmarshal mod_data: %v", err)
	}
	if got["tier"] != "steam" {
		t.Errorf("mod_data[tier] = %v, want %q", got["tier"], "steam")
	}
}

// A re-upsert with no mod-specific fields must reset mod_data to a valid empty JSONB
// object, not leave the previous call's data behind — a table DEFAULT only covers a first
// insert, so this has to go through the update path to actually exercise the Go-side
// encoding of a nil map.
func TestUpsertMachineType_NilModDataResetsToEmptyObjectOnUpdate(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")

	populated := model.MachineTypeDef{
		ModID: "testmod", MachineID: "plain_machine", Name: "Plain",
		ModData: map[string]any{"tier": "steam"},
	}
	if err := d.UpsertMachineType(ctx, populated); err != nil {
		t.Fatalf("upsert (populated): %v", err)
	}

	empty := model.MachineTypeDef{ModID: "testmod", MachineID: "plain_machine", Name: "Plain"}
	if err := d.UpsertMachineType(ctx, empty); err != nil {
		t.Fatalf("upsert (empty): %v", err)
	}

	var modData []byte
	if err := d.Pool.QueryRow(ctx,
		`SELECT mod_data FROM machine_types WHERE mod_id = $1 AND machine_id = $2`,
		"testmod", "plain_machine").Scan(&modData); err != nil {
		t.Fatalf("select: %v", err)
	}
	if string(modData) != "{}" {
		t.Errorf("mod_data = %s, want {} after re-upsert with no mod-specific fields", modData)
	}
}

// ImportRecipe must persist mod_data on the recipe row, and must keep a fluid output's ref
// in the fluid columns (not the item columns) — a naive "mod:id" ref string would still
// write without error but land nowhere a plugin or the solver looks for a fluid.
func TestImportRecipe_StoresModDataAndFluidOutput(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "iron_furnace")

	rec := model.NormalizedRecipe{
		ModID:       "testmod",
		SourceModID: "testmod",
		MachineID:   "iron_furnace",
		Duration:    100,
		ModData:     map[string]any{"heat": float64(500)},
		Inputs:      []resource.IO{itemIO("minecraft", "iron_ore", 1)},
		Outputs:     []resource.IO{fluidIO("testmod", "molten_iron", 1000)},
		ContentHash: uuid.NewString(),
	}
	imported, err := d.ImportRecipe(ctx, rec)
	if err != nil {
		t.Fatalf("import recipe: %v", err)
	}
	if !imported {
		t.Fatal("expected a new recipe to be imported")
	}

	var recipeID uuid.UUID
	var modData []byte
	if err := d.Pool.QueryRow(ctx,
		`SELECT id, mod_data FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&recipeID, &modData); err != nil {
		t.Fatalf("select recipe: %v", err)
	}
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(), `DELETE FROM recipes WHERE id = $1`, recipeID); err != nil {
			t.Errorf("cleanup recipe %s: %v", recipeID, err)
		}
	})

	var got map[string]any
	if err := json.Unmarshal(modData, &got); err != nil {
		t.Fatalf("unmarshal mod_data: %v", err)
	}
	if got["heat"] != float64(500) {
		t.Errorf("mod_data[heat] = %v, want 500", got["heat"])
	}

	var fluidModID, fluidID string
	if err := d.Pool.QueryRow(ctx,
		`SELECT fluid_mod_id, fluid_id FROM recipe_fluid_outputs WHERE recipe_id = $1`, recipeID,
	).Scan(&fluidModID, &fluidID); err != nil {
		t.Fatalf("select fluid output: %v", err)
	}
	if fluidModID != "testmod" || fluidID != "molten_iron" {
		t.Errorf("fluid output = %s:%s, want testmod:molten_iron", fluidModID, fluidID)
	}

	// — that would silently miss both the solver and any plugin looking for it as a fluid.
	var itemLeakCount int
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM recipe_item_outputs WHERE recipe_id = $1 AND item_id = 'molten_iron'`, recipeID,
	).Scan(&itemLeakCount); err != nil {
		t.Fatalf("select item outputs: %v", err)
	}
	if itemLeakCount != 0 {
		t.Errorf("fluid output leaked into recipe_item_outputs, count=%d", itemLeakCount)
	}
}

// A recipe referencing a machine no mod ever declared must be rejected, not silently backed
// by an auto-created stub row (which would have no mod_data and so silently drop costs from
// the solve).
func TestImportRecipe_RejectsUndeclaredMachine(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")

	rec := model.NormalizedRecipe{
		ModID:       "testmod",
		SourceModID: "testmod",
		MachineID:   "ghost_machine",
		Duration:    100,
		Inputs:      []resource.IO{itemIO("minecraft", "iron_ore", 1)},
		Outputs:     []resource.IO{itemIO("minecraft", "iron_ingot", 1)},
		ContentHash: uuid.NewString(),
	}
	imported, err := d.ImportRecipe(ctx, rec)
	if err == nil {
		t.Fatal("expected an error for a recipe referencing an undeclared machine")
	}
	if imported {
		t.Error("imported = true, want false on rejection")
	}

	var machineCount int
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM machine_types WHERE mod_id = $1 AND machine_id = $2`,
		"testmod", "ghost_machine",
	).Scan(&machineCount); err != nil {
		t.Fatalf("select machine_types: %v", err)
	}
	if machineCount != 0 {
		t.Errorf("machine_types row count = %d, want 0 — a stub must not be created", machineCount)
	}

	var recipeCount int
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&recipeCount); err != nil {
		t.Fatalf("select recipes: %v", err)
	}
	if recipeCount != 0 {
		t.Errorf("recipe row count = %d, want 0 — the whole transaction must roll back", recipeCount)
	}
}

// A re-import under a known content_hash keeps the row and its id, and refreshes its
// mod_data.
func TestImportRecipe_RefreshesModDataOnContentHashHit(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "refreshmod")
	seedMachineType(t, d, "refreshmod", "smelter")

	rec := model.NormalizedRecipe{
		ModID:       "refreshmod",
		SourceModID: "refreshmod",
		MachineID:   "smelter",
		Duration:    100,
		ModData:     map[string]any{"energy_per_tick": float64(128)},
		Inputs:      []resource.IO{itemIO("minecraft", "iron_ore", 1)},
		ContentHash: uuid.NewString(),
	}
	imported, err := d.ImportRecipe(ctx, rec)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if !imported {
		t.Fatal("expected the first import to create the recipe")
	}

	var firstID uuid.UUID
	if err := d.Pool.QueryRow(ctx,
		`SELECT id FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&firstID); err != nil {
		t.Fatalf("select recipe: %v", err)
	}
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(), `DELETE FROM recipes WHERE id = $1`, firstID); err != nil {
			t.Errorf("cleanup recipe %s: %v", firstID, err)
		}
	})

	// Same content_hash by construction, different mod_data.
	rec.ModData = map[string]any{"energy_per_tick": float64(32)}
	imported, err = d.ImportRecipe(ctx, rec)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if imported {
		t.Error("imported = true on a content_hash hit, want false — the row must be reused, not duplicated")
	}

	var count int
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&count); err != nil {
		t.Fatalf("count recipes: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d rows for the content hash, want exactly 1", count)
	}

	var secondID uuid.UUID
	var modData []byte
	if err := d.Pool.QueryRow(ctx,
		`SELECT id, mod_data FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&secondID, &modData); err != nil {
		t.Fatalf("select refreshed recipe: %v", err)
	}
	if secondID != firstID {
		t.Errorf("recipe id changed from %s to %s; a referenced row must keep its id", firstID, secondID)
	}
	var got map[string]any
	if err := json.Unmarshal(modData, &got); err != nil {
		t.Fatalf("unmarshal mod_data: %v", err)
	}
	if got["energy_per_tick"] != float64(32) {
		t.Errorf("mod_data[energy_per_tick] = %v, want the corrected 32", got["energy_per_tick"])
	}
}

// recipe_item_outputs stores item_mod_id/item_id NOT NULL, and parseItemIO leaves both nil
// when a recipe names a tag.
func TestImportRecipe_RejectsTagOnlyItemOutput(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "tagoutmod")
	seedMachineType(t, d, "tagoutmod", "press")

	rec := model.NormalizedRecipe{
		ModID:       "tagoutmod",
		SourceModID: "tagoutmod",
		MachineID:   "press",
		Duration:    100,
		Inputs:      []resource.IO{itemIO("minecraft", "iron_ore", 1)},
		Outputs:     []resource.IO{tagIO(resource.KindItem, "c:ingots/iron", 1)},
		ContentHash: uuid.NewString(),
	}

	imported, err := d.ImportRecipe(ctx, rec)
	if err == nil {
		t.Fatal("want an error for a tag-only item output, got nil")
	}
	if imported {
		t.Error("imported = true for a rejected recipe")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Errorf("error %q does not say the output is a tag", err)
	}

	var count int
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM recipes WHERE content_hash = $1`, rec.ContentHash,
	).Scan(&count); err != nil {
		t.Fatalf("count recipes: %v", err)
	}
	if count != 0 {
		t.Errorf("got %d rows for the rejected recipe, want 0 — the transaction must roll back", count)
	}
}
