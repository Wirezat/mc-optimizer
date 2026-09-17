package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// A confirmed line must keep the plugin config it was solved under. Without it
// the row falls back to the save-wide config, so unlocking a higher upgrade
// tier later re-evaluates an existing line against variants its stored
// variant_id is not part of.
func TestSolveResultToContentsFreezesTheConfigPerGroup(t *testing.T) {
	miConfig := json.RawMessage(`{"max_upgrade":"modern_industrialization:advanced_upgrade"}`)
	result := solver.SolveResult{
		MachineGroups: []solver.MachineGroupDraft{
			{
				MachineMod: "modern_industrialization", MachineID: "macerator",
				RecipeID:  "11111111-1111-1111-1111-111111111111",
				PluginMod: "modern_industrialization",
				VariantID: "modern_industrialization:advanced_upgrade-x3",
				Count:     2, ExactCount: solver.NewRational(3, 2),
			},
			{
				MachineMod: "minecraft", MachineID: "furnace",
				RecipeID:  "22222222-2222-2222-2222-222222222222",
				PluginMod: "minecraft",
				VariantID: "default",
				Count:     1, ExactCount: solver.NewRational(1, 1),
			},
		},
	}
	configs := map[string]json.RawMessage{"modern_industrialization": miConfig}

	_, groups, err := solveResultToContents(result, configs)
	if err != nil {
		t.Fatalf("solveResultToContents: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}

	byMachine := map[string]string{}
	for _, g := range groups {
		byMachine[g.MachineID] = string(g.ModConfig)
	}
	if got := byMachine["macerator"]; got != string(miConfig) {
		t.Errorf("macerator mod_config = %q, want the config it was solved under %q", got, miConfig)
	}
	// A mod without a plugin contributes no config; the row stays empty and the
	// save-wide fallback applies, which is also empty.
	if got := byMachine["furnace"]; got != "" {
		t.Errorf("furnace mod_config = %q, want empty for a mod with no plugin", got)
	}
}

// Scaling a draft before it is saved multiplies what the line produces and
// consumes, and lets a part-loaded group fill up.
func TestScaleSolveResult(t *testing.T) {
	item := solver.ItemRef{ModID: "minecraft", ItemID: "copper_ingot"}
	result := solver.SolveResult{
		MachineGroups: []solver.MachineGroupDraft{
			{MachineID: "macerator", Count: 2, ExactCount: solver.NewRational(3, 2)},
			{MachineID: "furnace", Count: 1, ExactCount: solver.RationalFromInt(1)},
		},
		IOProfile: solver.IOProfile{
			Inputs:  []solver.IOEntry{{Item: item, Rate: solver.NewRational(1, 2)}},
			Outputs: []solver.IOEntry{{Item: item, Rate: solver.RationalFromInt(4)}},
		},
		ActualRate: solver.RationalFromInt(4),
	}

	scaled := scaleSolveResult(result, solver.RationalFromInt(2))

	if got := scaled.MachineGroups[0]; got.Count != 3 || !got.Utilization.Eq(solver.RationalFromInt(1)) {
		t.Errorf("partial group scaled to count=%d util=%v, want 3 at 100%%", got.Count, got.Utilization)
	}
	if got := scaled.MachineGroups[1].Count; got != 2 {
		t.Errorf("full group count = %d, want 2", got)
	}
	if got := scaled.IOProfile.Inputs[0].Rate; !got.Eq(solver.RationalFromInt(1)) {
		t.Errorf("input rate = %v, want 1", got)
	}
	if got := scaled.IOProfile.Outputs[0].Rate; !got.Eq(solver.RationalFromInt(8)) {
		t.Errorf("output rate = %v, want 8", got)
	}
	if got := scaled.ActualRate; !got.Eq(solver.RationalFromInt(8)) {
		t.Errorf("actual rate = %v, want 8", got)
	}
	if result.MachineGroups[0].Count != 2 {
		t.Error("input result was mutated")
	}
}

// Scaling a saved line keeps every row's identity — the groups are updated in
// place, so build state survives.
func TestScalePLRows(t *testing.T) {
	groups := []*model.MachineGroup{
		{ID: uuid.New(), Count: 2, ExactCountNum: 3, ExactCountDen: 2, BuiltCount: 2},
	}
	ios := []*model.PLIO{{ID: uuid.New(), RateNum: 1, RateDen: 2}}

	rateNum, rateDen, sg, sio, err := scalePLRows(4, 1, groups, ios, solver.RationalFromInt(2))
	if err != nil {
		t.Fatalf("scalePLRows: %v", err)
	}
	if rateNum != 8 || rateDen != 1 {
		t.Errorf("line rate = %d/%d, want 8/1", rateNum, rateDen)
	}
	if sg[0].ID != groups[0].ID || sg[0].Count != 3 || sg[0].ExactNum != 3 || sg[0].ExactDen != 1 {
		t.Errorf("group = %+v, want id kept, count 3, exact 3/1", sg[0])
	}
	if sio[0].ID != ios[0].ID || sio[0].RateNum != 1 || sio[0].RateDen != 1 {
		t.Errorf("io = %+v, want id kept, rate 1/1", sio[0])
	}
}

// Shrinking a line below what is already standing in the world is refused:
// the build state is the one thing the app cannot recompute.
func TestScalePLRows_RefusesToShrinkBelowBuilt(t *testing.T) {
	groups := []*model.MachineGroup{
		{ID: uuid.New(), MachineID: "macerator", Count: 4, ExactCountNum: 4, ExactCountDen: 1, BuiltCount: 3},
	}

	_, _, _, _, err := scalePLRows(4, 1, groups, nil, solver.NewRational(1, 2))

	if !errors.Is(err, ErrBuiltCountExceeded) {
		t.Fatalf("err = %v, want ErrBuiltCountExceeded", err)
	}
}
