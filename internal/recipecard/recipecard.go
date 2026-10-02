// Package recipecard renders solver.RecipeRow as the crafting-grid card of the recipe endpoints.
package recipecard

import (
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

type Card struct {
	ID            string  `json:"id"`
	MachineModID  string  `json:"machine_mod_id"`
	MachineID     string  `json:"machine_id"`
	DurationTicks int     `json:"duration_ticks"`
	Inputs        []Entry `json:"inputs"`
	Outputs       []Entry `json:"outputs"`
}

type Entry struct {
	resource.IO
	X *int16 `json:"x,omitempty"`
	Y *int16 `json:"y,omitempty"`
}

// Build converts one solver.RecipeRow into a Card.
func Build(r *solver.RecipeRow) Card {
	card := Card{
		ID: r.ID, MachineModID: r.MachineMod, MachineID: r.MachineID,
		DurationTicks: r.DurationTicks,
		Inputs:        make([]Entry, 0, len(r.Inputs)),
		Outputs:       make([]Entry, 0, len(r.Outputs)),
	}
	for _, in := range r.Inputs {
		card.Inputs = append(card.Inputs, Entry{IO: in})
	}
	for _, out := range r.Outputs {
		card.Outputs = append(card.Outputs, Entry{IO: out})
	}
	return card
}

// ApplySlotLayout places a side's entries on machine slots of their kind, only if all fit.
func ApplySlotLayout(card *Card, slots []*model.MachineSlot) {
	placeSide(card.Inputs, slots, "_input")
	placeSide(card.Outputs, slots, "_output")
}

func placeSide(entries []Entry, slots []*model.MachineSlot, suffix string) {
	byKind := map[resource.Kind][]int{}
	var kinds []resource.Kind
	for i, e := range entries {
		k := e.Ref.Kind.Or()
		if _, seen := byKind[k]; !seen {
			kinds = append(kinds, k)
		}
		byKind[k] = append(byKind[k], i)
	}
	picked := map[resource.Kind][]*model.MachineSlot{}
	for _, k := range kinds {
		p, ok := resolveSlots(slots, string(k)+suffix, len(byKind[k]))
		if !ok {
			return
		}
		picked[k] = p
	}
	for _, k := range kinds {
		for j, i := range byKind[k] {
			entries[i].X, entries[i].Y = picked[k][j].SlotX, picked[k][j].SlotY
		}
	}
}

// resolveSlots picks the first n slots of slotType, skipping fuel slots.
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
