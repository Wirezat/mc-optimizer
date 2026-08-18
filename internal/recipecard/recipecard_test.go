package recipecard

import (
	"encoding/json"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/solver"
)

func strp(s string) *string { return &s }

func TestBuild_ItemAndTagInputs(t *testing.T) {
	row := &solver.RecipeRow{
		ID: "r1", MachineMod: "mi", MachineID: "compressor",
		DurationTicks: 100, EUPerTick: 32, TotalEU: 3200,
		ItemInputs: []solver.RecipeRowItemIO{
			{ItemModID: strp("mi"), ItemID: strp("iron_ingot"), AmountNum: 2, AmountDen: 1},
			{TagID: strp("c:dusts/coal"), TagName: strp("c:dusts/coal"), AmountNum: 1, AmountDen: 1, NonConsuming: true},
		},
		ItemOutputs: []solver.RecipeRowItemIO{
			{ItemModID: strp("mi"), ItemID: strp("steel_ingot"), AmountNum: 1, AmountDen: 1},
		},
		FluidInputs: []solver.RecipeRowFluidIO{
			{FluidModID: "mi", FluidID: "oxygen", AmountMB: 100},
		},
	}

	card := Build(row)

	if card.ID != "r1" || card.MachineModID != "mi" || card.MachineID != "compressor" {
		t.Fatalf("machine/id fields wrong: %+v", card)
	}
	if card.DurationTicks != 100 || card.EUPerTick != 32 || card.TotalEU != 3200 {
		t.Fatalf("timing/energy fields wrong: %+v", card)
	}
	if len(card.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(card.Inputs))
	}
	if card.Inputs[0].ItemModID != "mi" || card.Inputs[0].ItemID != "iron_ingot" || card.Inputs[0].Amount != 2 {
		t.Errorf("item input wrong: %+v", card.Inputs[0])
	}
	if card.Inputs[0].TagName != "" {
		t.Errorf("item input should have no tag_name, got %q", card.Inputs[0].TagName)
	}
	if card.Inputs[1].TagName != "c:dusts/coal" || !card.Inputs[1].NonConsuming {
		t.Errorf("tag input wrong: %+v", card.Inputs[1])
	}
	if card.Inputs[1].ItemModID != "" || card.Inputs[1].ItemID != "" {
		t.Errorf("tag input should have no item_mod_id/item_id, got %+v", card.Inputs[1])
	}
	if len(card.Outputs) != 1 || card.Outputs[0].ItemID != "steel_ingot" || card.Outputs[0].Amount != 1 {
		t.Errorf("output wrong: %+v", card.Outputs)
	}
	if len(card.FluidInputs) != 1 || card.FluidInputs[0].FluidID != "oxygen" || card.FluidInputs[0].AmountMB != 100 {
		t.Errorf("fluid input wrong: %+v", card.FluidInputs)
	}
	if card.FluidOutputs == nil || len(card.FluidOutputs) != 0 {
		t.Errorf("expected empty (not nil) fluid_outputs, got %+v", card.FluidOutputs)
	}

	// Round-trip through JSON to catch struct-tag typos the field assertions above wouldn't.
	b, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "machine_mod_id", "machine_id", "duration_ticks", "eu_per_tick", "total_eu", "inputs", "outputs", "fluid_inputs", "fluid_outputs"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("json output missing key %q: %v", key, raw)
		}
	}
}

func TestBuild_FractionalAmount(t *testing.T) {
	row := &solver.RecipeRow{
		ID: "r2", MachineMod: "mi", MachineID: "distillery",
		ItemOutputs: []solver.RecipeRowItemIO{
			{ItemModID: strp("mi"), ItemID: strp("thing"), AmountNum: 1, AmountDen: 2},
		},
	}
	card := Build(row)
	if card.Outputs[0].Amount != 0.5 {
		t.Errorf("expected fractional amount 0.5, got %v", card.Outputs[0].Amount)
	}
}
