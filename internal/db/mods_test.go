package db

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// TestGroupMachines_FoldsImplementersIntoBase asserts groupMachines folds only real
// single-base implementers into their base's Variants (alphabetical, RecipeCount summed),
// while name-alike and multi-base machines stay ungrouped.
func TestGroupMachines_FoldsImplementersIntoBase(t *testing.T) {
	all := []*model.MachineType{
		{ModID: "testmod", MachineID: "compressor", Name: "compressor", RecipeCount: 5},
		{ModID: "testmod", MachineID: "bronze_compressor", Name: "bronze_compressor", RecipeCount: 3},
		{ModID: "testmod", MachineID: "steel_compressor", Name: "steel_compressor", RecipeCount: 0},
		{ModID: "testmod", MachineID: "implosion_compressor", Name: "implosion_compressor", RecipeCount: 3},
		{ModID: "testmod", MachineID: "electric_blast_furnace", Name: "electric_blast_furnace", RecipeCount: 7},
		{ModID: "testmod", MachineID: "multi_processing_array", Name: "multi_processing_array", RecipeCount: 2},
	}
	interfaces := []MachineInterface{
		{MachineModID: "testmod", MachineID: "bronze_compressor", BaseModID: "testmod", BaseMachineID: "compressor"},
		{MachineModID: "testmod", MachineID: "steel_compressor", BaseModID: "testmod", BaseMachineID: "compressor"},
		// Implements two bases — must stay ungrouped, not pollute either group.
		{MachineModID: "testmod", MachineID: "multi_processing_array", BaseModID: "testmod", BaseMachineID: "compressor"},
		{MachineModID: "testmod", MachineID: "multi_processing_array", BaseModID: "testmod", BaseMachineID: "electric_blast_furnace"},
	}

	machines := groupMachines(all, interfaces)

	var compressorRow, implosionRow, blastFurnaceRow, multiRow *model.MachineType
	for _, m := range machines {
		switch m.MachineID {
		case "compressor":
			compressorRow = m
		case "implosion_compressor":
			implosionRow = m
		case "electric_blast_furnace":
			blastFurnaceRow = m
		case "multi_processing_array":
			multiRow = m
		}
	}
	if compressorRow == nil {
		t.Fatal("expected a compressor row in the grouped listing")
	}
	if len(compressorRow.Variants) != 3 {
		t.Fatalf("expected 3 variants (bronze, compressor, steel), got %d", len(compressorRow.Variants))
	}
	wantOrder := []string{"bronze_compressor", "compressor", "steel_compressor"}
	for i, v := range compressorRow.Variants {
		if v.MachineID != wantOrder[i] {
			t.Errorf("variants[%d] = %q, want %q (alphabetical order)", i, v.MachineID, wantOrder[i])
		}
	}
	if compressorRow.RecipeCount != 8 {
		t.Errorf("expected summed recipe_count 8 (5 base + 3 bronze + 0 steel), got %d", compressorRow.RecipeCount)
	}
	for _, m := range machines {
		if m.MachineID == "bronze_compressor" || m.MachineID == "steel_compressor" {
			t.Errorf("implementer %q must not appear as its own top-level row", m.MachineID)
		}
	}
	if implosionRow == nil {
		t.Fatal("expected implosion_compressor as its own row (no implements edge, must not fold into compressor)")
	}
	if len(implosionRow.Variants) != 0 {
		t.Errorf("implosion_compressor must not be grouped (name similarity is not an implements edge), got %d variants", len(implosionRow.Variants))
	}

	if multiRow == nil {
		t.Fatal("expected multi_processing_array as its own top-level row (implements >1 base, not a tier variant)")
	}
	if len(multiRow.Variants) != 0 {
		t.Errorf("multi_processing_array must not itself be treated as a group, got %d variants", len(multiRow.Variants))
	}
	if multiRow.RecipeCount != 2 {
		t.Errorf("multi_processing_array's own recipe_count must be unchanged, got %d", multiRow.RecipeCount)
	}
	for _, v := range compressorRow.Variants {
		if v.MachineID == "multi_processing_array" {
			t.Error("multi_processing_array must not appear in compressor's Variants")
		}
	}
	if compressorRow.RecipeCount != 8 {
		t.Errorf("compressor's recipe_count must not include multi_processing_array's, got %d", compressorRow.RecipeCount)
	}
	if blastFurnaceRow == nil {
		t.Fatal("expected electric_blast_furnace as its own row (no valid single-base implementers)")
	}
	if len(blastFurnaceRow.Variants) != 0 {
		t.Errorf("electric_blast_furnace must not be grouped with multi_processing_array, got %d variants", len(blastFurnaceRow.Variants))
	}
	if blastFurnaceRow.RecipeCount != 7 {
		t.Errorf("electric_blast_furnace's recipe_count must not include multi_processing_array's, got %d", blastFurnaceRow.RecipeCount)
	}
}

func TestLookupMachinePluginMods(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	seedMod(t, d, "lmpm_host")
	seedMod(t, d, "lmpm_addon")
	seedMachineType(t, d, "lmpm_host", "own_machine")
	seedMachineType(t, d, "lmpm_addon", "adopted_machine")
	if _, err := d.Pool.Exec(ctx,
		`UPDATE machine_types SET ecosystem = $1 WHERE mod_id = $2 AND machine_id = $3`,
		"lmpm_host", "lmpm_addon", "adopted_machine"); err != nil {
		t.Fatalf("set ecosystem: %v", err)
	}

	got, err := d.LookupMachinePluginMods(ctx, []solver.MachineRef{
		{ModID: "lmpm_host", MachineID: "own_machine"},
		{ModID: "lmpm_addon", MachineID: "adopted_machine"},
	})
	if err != nil {
		t.Fatalf("LookupMachinePluginMods: %v", err)
	}
	want := map[string]string{
		"lmpm_host:own_machine":      "lmpm_host",
		"lmpm_addon:adopted_machine": "lmpm_host",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// An empty request must not reach the database at all.
func TestLookupMachinePluginModsEmpty(t *testing.T) {
	d := testDB(t)
	got, err := d.LookupMachinePluginMods(context.Background(), nil)
	if err != nil {
		t.Fatalf("LookupMachinePluginMods: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

// A mod's list row carries what the Mods page groups and counts by: its items, and every
// ecosystem its machines declare (its own id where a machine names none, the same fallback
// solver.PluginMod applies).
func TestListMods_CountsAndEcosystems(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	seedMod(t, d, "zz_test_host")
	seedMod(t, d, "zz_test_addon")
	seedMachineType(t, d, "zz_test_host", "press")
	seedMachineType(t, d, "zz_test_addon", "big_press")
	if _, err := d.Pool.Exec(ctx,
		`UPDATE machine_types SET ecosystem = 'zz_test_host' WHERE mod_id = 'zz_test_addon'`); err != nil {
		t.Fatalf("set ecosystem: %v", err)
	}
	for _, id := range []string{"cog", "gear"} {
		if _, err := d.Pool.Exec(ctx,
			`INSERT INTO items (mod_id, item_id) VALUES ('zz_test_host', $1)`, id); err != nil {
			t.Fatalf("seed item %s: %v", id, err)
		}
	}

	mods, err := d.ListMods(ctx)
	if err != nil {
		t.Fatalf("ListMods: %v", err)
	}
	byID := map[string]*model.Mod{}
	for _, m := range mods {
		byID[m.ModID] = m
	}

	host, addon := byID["zz_test_host"], byID["zz_test_addon"]
	if host == nil || addon == nil {
		t.Fatalf("seeded mods missing from ListMods")
	}
	if host.ItemCount != 2 {
		t.Errorf("host ItemCount = %d, want 2", host.ItemCount)
	}
	if addon.ItemCount != 0 {
		t.Errorf("addon ItemCount = %d, want 0", addon.ItemCount)
	}
	if len(host.Ecosystems) != 1 || host.Ecosystems[0] != "zz_test_host" {
		t.Errorf("host Ecosystems = %v, want [zz_test_host] (own id, no ecosystem set)", host.Ecosystems)
	}
	if len(addon.Ecosystems) != 1 || addon.Ecosystems[0] != "zz_test_host" {
		t.Errorf("addon Ecosystems = %v, want [zz_test_host]", addon.Ecosystems)
	}
}

func TestListMods_NoMachinesNoEcosystem(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "zz_test_bare")

	mods, err := d.ListMods(context.Background())
	if err != nil {
		t.Fatalf("ListMods: %v", err)
	}
	for _, m := range mods {
		if m.ModID == "zz_test_bare" {
			if len(m.Ecosystems) != 0 {
				t.Errorf("Ecosystems = %v, want empty", m.Ecosystems)
			}
			return
		}
	}
	t.Fatal("seeded mod missing from ListMods")
}
