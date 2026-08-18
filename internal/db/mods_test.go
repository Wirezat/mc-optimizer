package db

import (
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// TestGroupMachines_FoldsImplementersIntoBase asserts groupMachines folds
// only real single-base implementers into their base's Variants (alphabetical,
// RecipeCount summed), while name-alike and multi-base machines stay ungrouped.
func TestGroupMachines_FoldsImplementersIntoBase(t *testing.T) {
	all := []*model.MachineType{
		{ModID: "testmod", MachineID: "compressor", Name: "compressor", BaseEUPerTick: 10, RecipeCount: 5},
		{ModID: "testmod", MachineID: "bronze_compressor", Name: "bronze_compressor", BaseEUPerTick: 2, RecipeCount: 3},
		{ModID: "testmod", MachineID: "steel_compressor", Name: "steel_compressor", BaseEUPerTick: 4, RecipeCount: 0},
		{ModID: "testmod", MachineID: "implosion_compressor", Name: "implosion_compressor", BaseEUPerTick: 10, RecipeCount: 3},
		{ModID: "testmod", MachineID: "electric_blast_furnace", Name: "electric_blast_furnace", BaseEUPerTick: 10, RecipeCount: 7},
		{ModID: "testmod", MachineID: "multi_processing_array", Name: "multi_processing_array", BaseEUPerTick: 10, RecipeCount: 2},
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
