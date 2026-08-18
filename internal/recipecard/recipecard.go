// Package recipecard renders solver.RecipeRow into the JSON shape the item
// and fluid recipe-detail endpoints return: enough for the frontend to draw
// a crafting-grid card, deliberately carrying no display names (the
// frontend already has those cached).
package recipecard

import (
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

type Card struct {
	ID            string   `json:"id"`
	MachineModID  string   `json:"machine_mod_id"`
	MachineID     string   `json:"machine_id"`
	DurationTicks int      `json:"duration_ticks"`
	EUPerTick     int64    `json:"eu_per_tick"`
	TotalEU       int64    `json:"total_eu"`
	Inputs        []Input  `json:"inputs"`
	Outputs       []Output `json:"outputs"`
	FluidInputs   []Fluid  `json:"fluid_inputs"`
	FluidOutputs  []Fluid  `json:"fluid_outputs"`
}

// Input is a concrete item (item_mod_id+item_id set, tag_name empty) or a
// tag slot (tag_name set, item_mod_id/item_id empty) — never both. X/Y are
// only set by ApplySlotLayout; the frontend falls back to its own grid
// layout when they're absent.
type Input struct {
	ItemModID    string  `json:"item_mod_id,omitempty"`
	ItemID       string  `json:"item_id,omitempty"`
	TagName      string  `json:"tag_name,omitempty"`
	Amount       float64 `json:"amount"`
	NonConsuming bool    `json:"non_consuming,omitempty"`
	X            *int16  `json:"x,omitempty"`
	Y            *int16  `json:"y,omitempty"`
}

type Output struct {
	ItemModID string  `json:"item_mod_id"`
	ItemID    string  `json:"item_id"`
	Amount    float64 `json:"amount"`
	X         *int16  `json:"x,omitempty"`
	Y         *int16  `json:"y,omitempty"`
}

type Fluid struct {
	FluidModID string `json:"fluid_mod_id"`
	FluidID    string `json:"fluid_id"`
	AmountMB   int64  `json:"amount_mb"`
	X          *int16 `json:"x,omitempty"`
	Y          *int16 `json:"y,omitempty"`
}

// Build converts one solver.RecipeRow into a Card. Pure function — no I/O —
// so it's tested directly without a DB.
func Build(r *solver.RecipeRow) Card {
	card := Card{
		ID: r.ID, MachineModID: r.MachineMod, MachineID: r.MachineID,
		DurationTicks: r.DurationTicks, EUPerTick: r.EUPerTick, TotalEU: r.TotalEU,
		Inputs:       make([]Input, 0, len(r.ItemInputs)),
		Outputs:      make([]Output, 0, len(r.ItemOutputs)),
		FluidInputs:  make([]Fluid, 0, len(r.FluidInputs)),
		FluidOutputs: make([]Fluid, 0, len(r.FluidOutputs)),
	}
	for _, in := range r.ItemInputs {
		v := Input{
			Amount:       float64(in.AmountNum) / float64(in.AmountDen),
			NonConsuming: in.NonConsuming,
		}
		if in.TagName != nil {
			v.TagName = *in.TagName
		} else {
			if in.ItemModID != nil {
				v.ItemModID = *in.ItemModID
			}
			if in.ItemID != nil {
				v.ItemID = *in.ItemID
			}
		}
		card.Inputs = append(card.Inputs, v)
	}
	for _, o := range r.ItemOutputs {
		v := Output{Amount: float64(o.AmountNum) / float64(o.AmountDen)}
		if o.ItemModID != nil {
			v.ItemModID = *o.ItemModID
		}
		if o.ItemID != nil {
			v.ItemID = *o.ItemID
		}
		card.Outputs = append(card.Outputs, v)
	}
	for _, fi := range r.FluidInputs {
		card.FluidInputs = append(card.FluidInputs, Fluid{FluidModID: fi.FluidModID, FluidID: fi.FluidID, AmountMB: fi.AmountMB})
	}
	for _, fo := range r.FluidOutputs {
		card.FluidOutputs = append(card.FluidOutputs, Fluid{FluidModID: fo.FluidModID, FluidID: fo.FluidID, AmountMB: fo.AmountMB})
	}
	return card
}

// ApplySlotLayout pairs card's inputs/outputs with slots (the owning
// machine's slot layout, in whatever order the DB returned them) and, if
// every one of them resolves, sets their X/Y. Item and fluid I/O on the same
// side (inputs or outputs) are treated as one group — a partial layout (some
// positioned, some not) is worse than the frontend's uniform grid fallback,
// so either the whole side gets coordinates or none of it does.
//
// TODO: recipe I/O has no stored ordering of its own (see loadRecipeIO in
// internal/db/recipes.go) — this pairs recipe input N with the machine's
// Nth non-fuel slot of the matching type in DB-return order, which usually
// matches import order but isn't guaranteed. Revisit if that ever causes a
// visibly wrong pairing.
//
// Also note: coverage is now as complete as it can be from a fixed x/y grid.
// Vanilla machines all have slot_x/slot_y (docs/vanilla.yml). For Modern
// Industrialization, the 14 machines registered via
// SingleBlockCraftingMachines.registerMachineTiers() (+ their bronze_/steel_
// tier variants — 27 of 45 MI machines) have coordinates extracted from that
// source file's addSlot/addSlots calls by scripts/import/inject_mi_slots.py.
// Extended Industrialization's 4 single-block machines (bending_machine,
// alloy_smelter, canning_machine, composter, + tier variants — all 10 EIO
// machines this project tracks) and Industrialization Overdrive's
// pyrolyse_oven are covered the same way by
// scripts/import/inject_eio_io_slots.py.
//
// What's NOT covered, and never will be by this mechanism: MI's
// SingleBlockSpecialMachines.java machines (boilers/generators/storage —
// not in this project's machine list anyway) and every multiblock (MI's
// MultiblockMachines.java: electric_blast_furnace, distillation_tower,
// fusion_reactor, the steam boilers, etc.; IO's multi_processing_array).
// Multiblocks source their items/fluids from hatch blocks placed in a 3D
// structure, not a fixed in-GUI slot grid — MI's own CraftingMultiblockGui
// confirms its GUI has no slots at all, just progress/EU. There is no
// coordinate data to extract for these; they fall back to the grid layout
// permanently, not just until someone gets around to it.
func ApplySlotLayout(card *Card, slots []*model.MachineSlot) {
	if itemIn, ok := resolveSlots(slots, "item_input", len(card.Inputs)); ok {
		if fluidIn, ok := resolveSlots(slots, "fluid_input", len(card.FluidInputs)); ok {
			for i := range card.Inputs {
				card.Inputs[i].X, card.Inputs[i].Y = itemIn[i].SlotX, itemIn[i].SlotY
			}
			for i := range card.FluidInputs {
				card.FluidInputs[i].X, card.FluidInputs[i].Y = fluidIn[i].SlotX, fluidIn[i].SlotY
			}
		}
	}
	if itemOut, ok := resolveSlots(slots, "item_output", len(card.Outputs)); ok {
		if fluidOut, ok := resolveSlots(slots, "fluid_output", len(card.FluidOutputs)); ok {
			for i := range card.Outputs {
				card.Outputs[i].X, card.Outputs[i].Y = itemOut[i].SlotX, itemOut[i].SlotY
			}
			for i := range card.FluidOutputs {
				card.FluidOutputs[i].X, card.FluidOutputs[i].Y = fluidOut[i].SlotX, fluidOut[i].SlotY
			}
		}
	}
}

// resolveSlots picks the first n usable slots of slotType, in the order
// given (fuel slots are burn-fuel, never a recipe I/O, so they're skipped).
// ok is false — and the pick unusable — if there aren't n of them, or any of
// the first n is missing x/y.
func resolveSlots(slots []*model.MachineSlot, slotType string, n int) (picked []*model.MachineSlot, ok bool) {
	if n == 0 {
		return nil, true
	}
	for _, s := range slots {
		if s.SlotType != slotType || (s.Label != nil && *s.Label == "fuel") {
			continue
		}
		if s.SlotX == nil || s.SlotY == nil {
			return nil, false
		}
		picked = append(picked, s)
		if len(picked) == n {
			return picked, true
		}
	}
	return nil, false
}
