// Package recipecard renders solver.RecipeRow into the JSON shape the item
// and fluid recipe-detail endpoints return, for the frontend's crafting-grid
// card.
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
	Inputs        []Input  `json:"inputs"`
	Outputs       []Output `json:"outputs"`
	FluidInputs   []Fluid  `json:"fluid_inputs"`
	FluidOutputs  []Fluid  `json:"fluid_outputs"`
}

// Input is a concrete item (item_mod_id+item_id set, tag_name empty) or a
// tag slot (tag_name set, item_mod_id/item_id empty), never both. X/Y are
// set only by ApplySlotLayout.
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

// Build converts one solver.RecipeRow into a Card.
func Build(r *solver.RecipeRow) Card {
	card := Card{
		ID: r.ID, MachineModID: r.MachineMod, MachineID: r.MachineID,
		DurationTicks: r.DurationTicks,
		Inputs:        make([]Input, 0, len(r.ItemInputs)),
		Outputs:       make([]Output, 0, len(r.ItemOutputs)),
		FluidInputs:   make([]Fluid, 0, len(r.FluidInputs)),
		FluidOutputs:  make([]Fluid, 0, len(r.FluidOutputs)),
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

// ApplySlotLayout pairs card's inputs/outputs with the owning machine's
// slots, setting X/Y only when every item on a side (inputs, or outputs)
// resolves to one. Multiblocks (MI's MultiblockMachines.java, IO's
// multi_processing_array) have no fixed slot grid and never resolve, always
// falling back to the frontend's grid layout.
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

// resolveSlots picks the first n slots of slotType, skipping fuel slots. ok
// is false if fewer than n exist, or any picked slot has no x/y.
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
