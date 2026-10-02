package solver

import (
	"encoding/json"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// RecipeRow is the lean recipe representation exchanged between the DB and solver.
type RecipeRow struct {
	ID            string
	MachineMod    string
	MachineID     string
	DurationTicks int
	// ModData is the recipe's opaque mod_data, passed through to a plugin unchanged.
	ModData      json.RawMessage
	ItemInputs   []RecipeRowItemIO
	ItemOutputs  []RecipeRowItemIO
	FluidInputs  []RecipeRowFluidIO
	FluidOutputs []RecipeRowFluidIO
}

type RecipeRowItemIO struct {
	ItemModID      *string
	ItemID         *string
	TagID          *string
	TagName        *string // resolved from the tags table
	AmountNum      int64
	AmountDen      int64
	ProbabilityNum int64
	ProbabilityDen int64
	NonConsuming   bool // true = reusable tool; not factored into consumption rates
}

type RecipeRowFluidIO struct {
	FluidModID     string
	FluidID        string
	TagName        *string
	AmountMB       int64
	ProbabilityNum int64
	ProbabilityDen int64
}

// Ref is the fluid, or the fluid tag, this row names.
func (f RecipeRowFluidIO) Ref() ResourceRef {
	if f.TagName != nil {
		return ResourceRef{TagRef: *f.TagName, Kind: resource.KindFluid}
	}
	return ResourceRef{ModID: f.FluidModID, ID: f.FluidID, Kind: resource.KindFluid}
}

// MachineSpec holds the subset of machine properties the solver needs.
type MachineSpec struct {
	ModID     string
	MachineID string
	Name      string
	// Ecosystem names the plugin's mod_id; empty means the machine's own.
	Ecosystem string
	ModData   json.RawMessage
}

// filterByActiveMods keeps only rows whose own MachineMod is active.
func filterByActiveMods(recipes []*RecipeRow, activeMods map[string]bool) []*RecipeRow {
	if activeMods == nil {
		return recipes
	}
	out := make([]*RecipeRow, 0, len(recipes))
	for _, r := range recipes {
		if activeMods[r.MachineMod] {
			out = append(out, r)
		}
	}
	return out
}
