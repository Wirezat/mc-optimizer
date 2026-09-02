package db

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

// TestMIPluginAgainstRealCatalog feeds a real, imported Modern
// Industrialization machine and recipe through the compiled plugin. The
// hand-built fixture in internal/plugins/mi_reference_test.go had spelled
// field names the way spec section 5 originally named them
// (energy_type, total_energy, fixed_recipe_energy_cap) instead of what the
// importer actually writes to mod_data - so it passed while evaluate()
// silently returned only the nominal variant against every real machine.
// This test reads the real columns instead of re-describing them, so it
// cannot drift from the importer the same way.
func TestMIPluginAgainstRealCatalog(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	// assembler is deliberately picked over e.g. bronze_compressor or
	// bronze_furnace: those exist in the catalog with upgradable=false, which
	// would make "only one variant" the correct answer and defeat this test.
	var machineData []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT mod_data FROM machine_types
		WHERE mod_id = 'modern_industrialization' AND machine_id = 'assembler'
	`).Scan(&machineData)
	if err != nil {
		t.Skipf("modern_industrialization/assembler not present in this DB: %v", err)
	}

	var recipeID string
	var durationTicks int64
	var recipeData []byte
	err = d.Pool.QueryRow(ctx, `
		SELECT id, duration_ticks, mod_data FROM recipes
		WHERE machine_mod_id = 'modern_industrialization' AND machine_id = 'assembler'
		ORDER BY id LIMIT 1
	`).Scan(&recipeID, &durationTicks, &recipeData)
	if err != nil {
		t.Skipf("no modern_industrialization/assembler recipe in this DB: %v", err)
	}

	src, err := os.ReadFile("../../mod-plugins/modern_industrialization/plugin/plugin.js")
	if err != nil {
		t.Fatalf("read MI plugin: %v", err)
	}
	prog, err := plugins.Compile("modern_industrialization", string(src))
	if err != nil {
		t.Fatalf("compile MI plugin: %v", err)
	}

	const bonus = int64(16)
	cfg := `{"max_upgrade":"modern_industrialization:advanced_upgrade"}`
	ec := plugins.EvalContext{
		Machine: plugins.EvalMachine{ModID: "modern_industrialization", MachineID: "assembler", Data: json.RawMessage(machineData)},
		Recipe: plugins.EvalRecipe{
			ID:            recipeID,
			DurationTicks: durationTicks,
			Data:          json.RawMessage(recipeData),
		},
		Config: json.RawMessage(cfg),
	}

	vs, err := prog.Evaluate(ctx, ec)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(vs) <= 1 {
		t.Fatalf("got %d variant(s) for an upgradable machine with upgrade_items configured, want more than 1 - "+
			"this is exactly the symptom of evaluate() gating on a field mod_data never has", len(vs))
	}

	// Recompute the expected cost for every returned variant straight from
	// the real mod_data columns this DB holds, independent of the plugin's
	// own arithmetic, using the field names the importer actually writes.
	var m struct {
		BaseEnergyPerTick int64 `json:"base_energy_per_tick"`
		MaxEnergyPerTick  int64 `json:"max_energy_per_tick"`
	}
	if err := json.Unmarshal(machineData, &m); err != nil {
		t.Fatalf("unmarshal machine mod_data: %v", err)
	}
	var r struct {
		EnergyPerTick int64 `json:"energy_per_tick"`
	}
	if err := json.Unmarshal(recipeData, &r); err != nil {
		t.Fatalf("unmarshal recipe mod_data: %v", err)
	}

	baseMax := m.MaxEnergyPerTick
	if baseMax <= 0 {
		baseMax = m.BaseEnergyPerTick
	}
	if baseMax <= 0 {
		baseMax = 32
	}
	total := r.EnergyPerTick * durationTicks

	sawBase := false
	for _, v := range vs {
		var installedBonus int64
		switch len(v.Items) {
		case 0:
			sawBase = true
		case 1:
			installedBonus = bonus * int64(v.Items[0].Count)
		default:
			t.Fatalf("variant %q installs %d item types, want 0 or 1", v.ID, len(v.Items))
		}

		wantPerTick := baseMax + installedBonus
		if total > 0 && wantPerTick > total {
			wantPerTick = total
		}
		wantTicks := (total + wantPerTick - 1) / wantPerTick // ceil division

		eu := costOf(t, v, "eu")
		if eu.Num != wantPerTick || eu.Den != 1 {
			t.Errorf("variant %q eu = %d/%d, want %d/1", v.ID, eu.Num, eu.Den, wantPerTick)
		}
		if v.Rate.Den != wantTicks || v.Rate.Num != 1 {
			t.Errorf("variant %q rate = %d/%d, want 1/%d", v.ID, v.Rate.Num, v.Rate.Den, wantTicks)
		}
	}
	if !sawBase {
		t.Error("no base (zero-item) variant among the results")
	}
}

// costOf mirrors internal/plugins' test helper of the same name; that one
// lives in package plugins and is not importable from here.
func costOf(t *testing.T, v plugins.Variant, resource string) plugins.Rational {
	t.Helper()
	for _, c := range v.Costs {
		if c.Resource == resource {
			return c.Amount
		}
	}
	t.Fatalf("variant %q has no %q cost", v.ID, resource)
	return plugins.Rational{}
}
