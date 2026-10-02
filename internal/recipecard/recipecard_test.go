package recipecard

import (
	"encoding/json"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

func i16p(v int16) *int16 { return &v }

func item(id string) Entry {
	return Entry{IO: resource.IO{Ref: resource.Ref{ID: id}}}
}

func fluid(id string) Entry {
	return Entry{IO: resource.IO{Ref: resource.Ref{ID: id, Kind: resource.KindFluid}}}
}

func slot(typ string, x, y int16, label string) *model.MachineSlot {
	s := &model.MachineSlot{SlotType: typ, SlotX: i16p(x), SlotY: i16p(y)}
	if label != "" {
		s.Label = &label
	}
	return s
}

func TestApplySlotLayout_PositionsWhenSlotsCoverAllIO(t *testing.T) {
	card := &Card{
		Inputs:  []Entry{item("a"), item("b")},
		Outputs: []Entry{item("c")},
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
	card := &Card{Inputs: []Entry{item("ore")}}
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
	card := &Card{Inputs: []Entry{item("a"), item("b")}}
	slots := []*model.MachineSlot{slot("item_input", 0, 0, "")}
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X != nil || card.Inputs[1].X != nil {
		t.Errorf("expected no positioning (insufficient slots), got %+v", card.Inputs)
	}
}

func TestApplySlotLayout_FallsBackWhenSlotMissingCoords(t *testing.T) {
	card := &Card{Inputs: []Entry{item("a")}}
	slots := []*model.MachineSlot{{SlotType: "item_input"}} // no SlotX/SlotY
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X != nil {
		t.Errorf("expected no positioning (slot missing coords), got %+v", card.Inputs[0])
	}
}

func TestApplySlotLayout_PartialSideFallsBackEntirely(t *testing.T) {
	card := &Card{
		Inputs: []Entry{item("a"), fluid("steam")},
	}
	slots := []*model.MachineSlot{slot("item_input", 0, 0, "")}
	ApplySlotLayout(card, slots)

	if card.Inputs[0].X != nil {
		t.Errorf("expected item input to stay unpositioned when its side is incomplete, got %+v", card.Inputs[0])
	}
}

func TestApplySlotLayout_NoSlots_NoOp(t *testing.T) {
	card := &Card{Inputs: []Entry{item("a")}}
	ApplySlotLayout(card, nil)
	if card.Inputs[0].X != nil {
		t.Errorf("expected no positioning with nil slots, got %+v", card.Inputs[0])
	}
}

func TestBuild_ItemAndTagInputs(t *testing.T) {
	row := &solver.RecipeRow{
		ID: "r1", MachineMod: "mi", MachineID: "compressor",
		DurationTicks: 100,
		Inputs: []resource.IO{
			{Ref: resource.Ref{ModID: "mi", ID: "iron_ingot"}, Amount: resource.NewRational(2, 1), Consumed: true},
			{Ref: resource.Ref{TagRef: "c:dusts/coal"}, Amount: resource.NewRational(1, 1)},
			{Ref: resource.Ref{ModID: "mi", ID: "oxygen", Kind: resource.KindFluid}, Amount: resource.NewRational(100, 1), Consumed: true},
		},
		Outputs: []resource.IO{
			{Ref: resource.Ref{ModID: "mi", ID: "steel_ingot"}, Amount: resource.NewRational(1, 1), Consumed: true},
		},
	}

	card := Build(row)

	if card.ID != "r1" || card.MachineModID != "mi" || card.MachineID != "compressor" {
		t.Fatalf("machine/id fields wrong: %+v", card)
	}
	if card.DurationTicks != 100 {
		t.Fatalf("duration field wrong: %+v", card)
	}
	if len(card.Inputs) != 3 {
		t.Fatalf("expected 3 inputs, got %d", len(card.Inputs))
	}
	if in := card.Inputs[0]; in.Ref.ModID != "mi" || in.Ref.ID != "iron_ingot" || in.Amount != resource.NewRational(2, 1) || !in.Consumed {
		t.Errorf("item input wrong: %+v", in)
	}
	if card.Inputs[0].Ref.TagRef != "" {
		t.Errorf("item input should have no tag_ref, got %q", card.Inputs[0].Ref.TagRef)
	}
	if card.Inputs[1].Ref.TagRef != "c:dusts/coal" || card.Inputs[1].Consumed {
		t.Errorf("tag input wrong: %+v", card.Inputs[1])
	}
	if card.Inputs[1].Ref.ModID != "" || card.Inputs[1].Ref.ID != "" {
		t.Errorf("tag input should have no mod_id/id, got %+v", card.Inputs[1])
	}
	if in := card.Inputs[2]; in.Ref.Kind != resource.KindFluid || in.Ref.ID != "oxygen" || in.Amount != resource.NewRational(100, 1) {
		t.Errorf("fluid input wrong: %+v", in)
	}
	if len(card.Outputs) != 1 || card.Outputs[0].Ref.ID != "steel_ingot" || card.Outputs[0].Amount != resource.NewRational(1, 1) {
		t.Errorf("output wrong: %+v", card.Outputs)
	}

	b, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "machine_mod_id", "machine_id", "duration_ticks", "inputs", "outputs"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("json output missing key %q: %v", key, raw)
		}
	}
	inputs, _ := raw["inputs"].([]any)
	if len(inputs) != 3 {
		t.Fatalf("json inputs = %v, want 3 entries", raw["inputs"])
	}
	for i, in := range inputs {
		entry, _ := in.(map[string]any)
		for _, key := range []string{"ref", "amount", "probability", "consumed"} {
			if _, ok := entry[key]; !ok {
				t.Errorf("json input %d missing key %q: %v", i, key, entry)
			}
		}
	}
}

func TestBuild_NoOutputsIsEmptyNotNil(t *testing.T) {
	card := Build(&solver.RecipeRow{ID: "r", MachineMod: "m", MachineID: "void"})
	if card.Outputs == nil || len(card.Outputs) != 0 {
		t.Errorf("expected empty (not nil) outputs, got %+v", card.Outputs)
	}
	b, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if outs, ok := raw["outputs"].([]any); !ok || len(outs) != 0 {
		t.Errorf("json outputs = %v, want []", raw["outputs"])
	}
}

func TestBuild_FractionalAmount(t *testing.T) {
	row := &solver.RecipeRow{
		ID: "r2", MachineMod: "mi", MachineID: "distillery",
		Outputs: []resource.IO{
			{Ref: resource.Ref{ModID: "mi", ID: "thing"}, Amount: resource.NewRational(1, 2), Consumed: true},
		},
	}
	card := Build(row)
	if card.Outputs[0].Amount != resource.NewRational(1, 2) {
		t.Errorf("expected fractional amount 1/2, got %v", card.Outputs[0].Amount)
	}
}

func TestBuild_FluidTagInput(t *testing.T) {
	card := Build(&solver.RecipeRow{
		ID: "r", MachineMod: "m", MachineID: "canner",
		Inputs: []resource.IO{{Ref: resource.Ref{TagRef: "c:honey", Kind: resource.KindFluid}, Amount: resource.NewRational(250, 1)}},
	})
	if len(card.Inputs) != 1 || card.Inputs[0].Ref.Kind != resource.KindFluid || card.Inputs[0].Ref.TagRef != "c:honey" || card.Inputs[0].Amount != resource.NewRational(250, 1) {
		t.Errorf("inputs = %+v, want fluid tag c:honey with 250 mB", card.Inputs)
	}
}
