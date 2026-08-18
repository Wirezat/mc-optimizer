// Package recipecard renders solver.RecipeRow into the JSON shape the item
// and fluid recipe-detail endpoints return: enough for the frontend to draw
// a crafting-grid card, deliberately carrying no display names (the
// frontend already has those cached).
package recipecard

import "github.com/Wirezat/production-optimizer/internal/solver"

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
// tag slot (tag_name set, item_mod_id/item_id empty) — never both.
type Input struct {
	ItemModID    string  `json:"item_mod_id,omitempty"`
	ItemID       string  `json:"item_id,omitempty"`
	TagName      string  `json:"tag_name,omitempty"`
	Amount       float64 `json:"amount"`
	NonConsuming bool    `json:"non_consuming,omitempty"`
}

type Output struct {
	ItemModID string  `json:"item_mod_id"`
	ItemID    string  `json:"item_id"`
	Amount    float64 `json:"amount"`
}

type Fluid struct {
	FluidModID string `json:"fluid_mod_id"`
	FluidID    string `json:"fluid_id"`
	AmountMB   int64  `json:"amount_mb"`
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
