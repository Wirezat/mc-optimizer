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
	ModData       json.RawMessage
	Inputs        []resource.IO
	Outputs       []resource.IO
}

// MachineSpec holds the subset of machine properties the solver needs.
type MachineSpec struct {
	ModID     string
	MachineID string
	Name      string
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
