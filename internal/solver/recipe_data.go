package solver

import "encoding/json"

// RecipeRow is the lean recipe representation exchanged between the DB and solver.
// Only the fields needed for rate calculations are present.
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
	AmountMB       int64
	ProbabilityNum int64
	ProbabilityDen int64
}

// MachineSpec holds the subset of machine properties the solver needs.
type MachineSpec struct {
	ModID     string
	MachineID string
	Name      string
	// Ecosystem names the mod_id whose plugin evaluates this machine.
	// Empty means the machine's own ModID.
	Ecosystem string
	// ModData is the machine's opaque mod_data.
	ModData json.RawMessage
}

// filterByActiveMods keeps only rows whose own MachineMod is active. Filtering is
// per candidate machine, not per recipe: a recipe with several machine candidates
// (base plus machine_interfaces implementers) survives if ANY candidate's mod is
// active, even when the base machine's mod is not.
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
