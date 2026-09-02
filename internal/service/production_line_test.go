package service

import (
	"encoding/json"
	"testing"

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
