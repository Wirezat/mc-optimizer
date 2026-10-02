package recipecard

import (
	"encoding/json"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

func strp(s string) *string { return &s }
func i16p(v int16) *int16   { return &v }

func slot(typ string, x, y int16, label string) *model.MachineSlot {
	s := &model.MachineSlot{SlotType: typ, SlotX: i16p(x), SlotY: i16p(y)}
	if label != "" {
		s.Label = &label
	}
	return s
}

func TestApplySlotLayout_PositionsWhenSlotsCoverAllIO(t *testing.T) {
	card := &Card{
		Inputs:  []Input{{ItemID: "a"}, {ItemID: "b"}},
		Outputs: []Output{{ItemID: "c"}},
	}
	slots := []*model.MachineSlot{
		slot("item_input", 0, 0, ""),
		slot("item_input", 1, 0, ""),
		slot("item_output", 4, 0, ""),
	}
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X == nil || *card.Inputs[0].X != 0 || *card.Inputs[0].Y != 0 {
		t.Errorf("input 0 not positioned: %+v", card.Inputs[0])
	}
	if card.Inputs[1].X == nil || *card.Inputs[1].X != 1 {
		t.Errorf("input 1 not positioned: %+v", card.Inputs[1])
	}
	if card.Outputs[0].X == nil || *card.Outputs[0].X != 4 {
		t.Errorf("output not positioned: %+v", card.Outputs[0])
	}
}

func TestApplySlotLayout_FuelSlotNeverUsedForRecipeInput(t *testing.T) {
	card := &Card{Inputs: []Input{{ItemID: "ore"}}}
	slots := []*model.MachineSlot{
		slot("item_input", 0, 0, "fuel"),
		slot("item_input", 0, 1, ""),
	}
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X == nil || *card.Inputs[0].Y != 1 {
		t.Errorf("expected the non-fuel slot to be picked, got %+v", card.Inputs[0])
	}
}

func TestApplySlotLayout_FallsBackWhenNotEnoughSlots(t *testing.T) {
	card := &Card{Inputs: []Input{{ItemID: "a"}, {ItemID: "b"}}}
	slots := []*model.MachineSlot{slot("item_input", 0, 0, "")}
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X != nil || card.Inputs[1].X != nil {
		t.Errorf("expected no positioning (insufficient slots), got %+v", card.Inputs)
	}
}

func TestApplySlotLayout_FallsBackWhenSlotMissingCoords(t *testing.T) {
	card := &Card{Inputs: []Input{{ItemID: "a"}}}
	slots := []*model.MachineSlot{{SlotType: "item_input"}} // no SlotX/SlotY
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X != nil {
		t.Errorf("expected no positioning (slot missing coords), got %+v", card.Inputs[0])
	}
}

func TestApplySlotLayout_PartialSideFallsBackEntirely(t *testing.T) {
	card := &Card{
		Inputs:      []Input{{ItemID: "a"}},
		FluidInputs: []Fluid{{FluidID: "steam"}},
	}
	slots := []*model.MachineSlot{slot("item_input", 0, 0, "")}
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X != nil {
		t.Errorf("expected item input to stay unpositioned when its side is incomplete, got %+v", card.Inputs[0])
	}
}

func TestApplySlotLayout_NoSlots_NoOp(t *testing.T) {
	card := &Card{Inputs: []Input{{ItemID: "a"}}}
	ApplySlotLayout(card, nil)
	if card.Inputs[0].X != nil {
		t.Errorf("expected no positioning with nil slots, got %+v", card.Inputs[0])
	}
}

func TestBuild_ItemAndTagInputs(t *testing.T) {
	row := &solver.RecipeRow{
		ID: "r1", MachineMod: "mi", MachineID: "compressor",
		DurationTicks: 100,
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
	if card.DurationTicks != 100 {
		t.Fatalf("duration field wrong: %+v", card)
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

	b, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "machine_mod_id", "machine_id", "duration_ticks", "inputs", "outputs", "fluid_inputs", "fluid_outputs"} {
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

func TestBuild_FluidTagInput(t *testing.T) {
	card := Build(&solver.RecipeRow{
		ID: "r", MachineMod: "m", MachineID: "canner",
		FluidInputs: []solver.RecipeRowFluidIO{{TagName: strp("c:honey"), AmountMB: 250}},
	})
	if len(card.FluidInputs) != 1 || card.FluidInputs[0].TagName != "c:honey" || card.FluidInputs[0].AmountMB != 250 {
		t.Errorf("fluid inputs = %+v, want tag c:honey with 250 mB", card.FluidInputs)
	}
}
