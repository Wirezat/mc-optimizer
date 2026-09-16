package plugins

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func miProgram(t *testing.T) *Program {
	t.Helper()
	src, err := os.ReadFile("../../mod-plugins/modern_industrialization/plugin/plugin.js")
	if err != nil {
		t.Fatalf("read MI plugin: %v", err)
	}
	prog, err := Compile("modern_industrialization", string(src))
	if err != nil {
		t.Fatalf("compile MI plugin: %v", err)
	}
	return prog
}

// mixerContext mirrors the documented reference scenario: MI mixer,
// bronze_dust, 128 EU/t base cap, 40 ticks, 5120 EU total (derived as
// 128 * 40, since a real recipe row never carries total_energy).
// Field names match what the importer actually writes to mod_data
// (verified against a live DB via
// `SELECT DISTINCT jsonb_object_keys(mod_data) FROM machine_types`) -
// there is no energy_type field on a machine, and recipes.mod_data only
// ever has energy_per_tick.
func mixerContext(config string) EvalContext {
	return EvalContext{
		Machine: EvalMachine{
			ModID: "modern_industrialization", MachineID: "mixer",
			Data: json.RawMessage(`{"base_energy_per_tick":128,"max_energy_per_tick":128,"upgradable":true}`),
		},
		Recipe: EvalRecipe{
			ID: "mixer-bronze-dust", DurationTicks: 40,
			Outputs: []Output{{Ref: "modern_industrialization:bronze_dust", Amount: Rational{1, 1}, Probability: Rational{1, 1}}},
			Data:    json.RawMessage(`{"energy_per_tick":128}`),
		},
		Config: json.RawMessage(config),
	}
}

func TestMIBaseVariantMatchesReference(t *testing.T) {
	vs, err := miProgram(t).Evaluate(context.Background(), mixerContext(`{"tier":"electric"}`))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	base := findVariant(t, vs, func(v Variant) bool { return len(v.Items) == 0 })
	if eu := costOf(t, base, "eu"); eu.Num != 128 || eu.Den != 1 {
		t.Errorf("base eu = %d/%d, want 128/1", eu.Num, eu.Den)
	}
}

// The reference value from the handoff: MI mixer, bronze_dust, three
// advanced upgrades on a 128 EU/t machine draw 176 EU/t. The config now
// names the tier rather than spelling out the item's bonus and cap, so this
// also proves the hardcoded table carries the right bonus.
func TestMIUpgradedVariantMatchesReference(t *testing.T) {
	cfg := `{"tier":"electric","max_upgrade":"modern_industrialization:advanced_upgrade"}`
	vs, err := miProgram(t).Evaluate(context.Background(), mixerContext(cfg))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	v := findVariant(t, vs, func(v Variant) bool {
		return len(v.Items) == 1 && v.Items[0].Count == 3
	})
	if eu := costOf(t, v, "eu"); eu.Num != 176 || eu.Den != 1 {
		t.Errorf("advanced x3 eu = %d/%d, want 176/1", eu.Num, eu.Den)
	}
	if v.Items[0].Ref != "modern_industrialization:advanced_upgrade" {
		t.Errorf("item ref = %q, want the configured tier", v.Items[0].Ref)
	}
}

// total_energy 200 on a 128 EU/t machine takes 2 ticks at 0 and at 3 upgrades.
func TestMIThinsDominatedUpgradeCounts(t *testing.T) {
	ec := EvalContext{
		Machine: EvalMachine{
			ModID: "modern_industrialization", MachineID: "mixer",
			Data: json.RawMessage(`{"base_energy_per_tick":128,"max_energy_per_tick":128,"upgradable":true}`),
		},
		Recipe: EvalRecipe{
			ID: "dominated-steps", DurationTicks: 2,
			Data: json.RawMessage(`{"energy_per_tick":100}`),
		},
		Config: json.RawMessage(`{"tier":"electric","max_upgrade":"modern_industrialization:advanced_upgrade"}`),
	}
	vs, err := miProgram(t).Evaluate(context.Background(), ec)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	base := findVariant(t, vs, func(v Variant) bool { return len(v.Items) == 0 })
	for _, v := range vs {
		if len(v.Items) == 0 {
			continue
		}
		if v.Rate == base.Rate {
			t.Errorf("variant %q runs at the base rate %d/%d and still draws %v",
				v.ID, base.Rate.Num, base.Rate.Den, v.Costs)
		}
	}
}

// An unknown or absent max_upgrade means no upgrades at all, never a guess
// at the mod's own default: the config says what the player has unlocked,
// and the plugin has no other source for that.
func TestMIUnknownMaxUpgradeYieldsBaseOnly(t *testing.T) {
	for name, cfg := range map[string]string{
		"absent":  `{"tier":"electric"}`,
		"unknown": `{"tier":"electric","max_upgrade":"modern_industrialization:not_an_upgrade"}`,
		"empty":   `{"tier":"electric","max_upgrade":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			vs, err := miProgram(t).Evaluate(context.Background(), mixerContext(cfg))
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if len(vs) != 1 {
				t.Fatalf("got %d variants, want exactly the base", len(vs))
			}
			if len(vs[0].Items) != 0 {
				t.Errorf("the single variant installs %d item kinds, want 0", len(vs[0].Items))
			}
		})
	}
}

// The worst case is a full 64-stack that never gets the craft down to a single
// tick: 65 entries, a quarter of the host's cap.
func TestMISingleTierSweepStaysBounded(t *testing.T) {
	ec := EvalContext{
		Machine: EvalMachine{
			ModID: "modern_industrialization", MachineID: "mixer",
			Data: json.RawMessage(`{"base_energy_per_tick":100,"max_energy_per_tick":100,"upgradable":true}`),
		},
		Recipe: EvalRecipe{
			ID: "long-recipe", DurationTicks: 1000,
			Data: json.RawMessage(`{"energy_per_tick":100}`),
		},
		Config: json.RawMessage(`{"tier":"electric","max_upgrade":"modern_industrialization:basic_upgrade"}`),
	}
	vs, err := miProgram(t).Evaluate(context.Background(), ec)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(vs) != 65 {
		t.Fatalf("got %d variants, want 65 (base plus a full 64 stack of one tier)", len(vs))
	}
	if len(vs) > maxVariants {
		t.Fatalf("got %d variants, host cap is %d", len(vs), maxVariants)
	}
	bases := 0
	for _, v := range vs {
		if len(v.Items) == 0 {
			bases++
		}
	}
	if bases != 1 {
		t.Fatalf("expected exactly one base variant, got %d", bases)
	}
}

// TestMIFixedRecipeEUCapBansOverDemand guards the field-name fix for the
// hard energy cap: the importer writes it to mod_data as
// fixed_recipe_eu_cap, not fixed_recipe_energy_cap. A machine whose recipe
// demands more than that cap must come back invalid.
func TestMIFixedRecipeEUCapBansOverDemand(t *testing.T) {
	ec := EvalContext{
		Machine: EvalMachine{
			ModID: "modern_industrialization", MachineID: "electric_blast_furnace",
			Data: json.RawMessage(`{"base_energy_per_tick":128,"max_energy_per_tick":128,"fixed_recipe_eu_cap":32,"upgradable":true}`),
		},
		Recipe: EvalRecipe{
			ID: "over-cap-recipe", DurationTicks: 10,
			Data: json.RawMessage(`{"energy_per_tick":100}`),
		},
		Config: json.RawMessage(`{"tier":"electric"}`),
	}
	vs, err := miProgram(t).Evaluate(context.Background(), ec)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	base := findVariant(t, vs, func(v Variant) bool { return len(v.Items) == 0 })
	if base.Valid {
		t.Fatalf("base variant demands 100 EU/t against a 32 EU/t fixed_recipe_eu_cap, want valid=false, got true")
	}
}

func findVariant(t *testing.T, vs []Variant, pred func(Variant) bool) Variant {
	t.Helper()
	for _, v := range vs {
		if pred(v) {
			return v
		}
	}
	t.Fatalf("no matching variant among %d", len(vs))
	return Variant{}
}

func costOf(t *testing.T, v Variant, resource string) Rational {
	t.Helper()
	for _, c := range v.Costs {
		if c.Resource == resource {
			return c.Amount
		}
	}
	t.Fatalf("variant %q has no %q cost", v.ID, resource)
	return Rational{}
}

// has_wizard drives whether the mod gets a config button at all, and the
// host resolves it structurally from the binding (Program.HasWizard). A
// rewritten wizard that loses its mount function would silently remove the
// button rather than fail loudly.
func TestMIPluginBindsWizard(t *testing.T) {
	if !miProgram(t).HasWizard() {
		t.Error("HasWizard() = false; MI's wizard.mount binding is gone or malformed")
	}
}

// A machine of another tier is vetoed, not merely ranked lower.
func TestMITierPinVetoesOtherTiers(t *testing.T) {
	macerator := func(machineID, tier string) EvalContext {
		return EvalContext{
			Machine: EvalMachine{
				ModID: "modern_industrialization", MachineID: machineID,
				Data: json.RawMessage(`{"base_energy_per_tick":32,"max_energy_per_tick":32,"upgradable":true}`),
			},
			Recipe: EvalRecipe{ID: "r", DurationTicks: 100, Data: json.RawMessage(`{"energy_per_tick":2}`)},
			Config: json.RawMessage(`{"tier":"` + tier + `"}`),
		}
	}
	tests := []struct {
		machineID, tier string
		want            bool
	}{
		{"bronze_macerator", "bronze", true},
		{"bronze_macerator", "electric", false},
		{"macerator", "electric", true},
		{"macerator", "bronze", false},
		// No prefixed blast furnace exists below electric, so steam stands in.
		{"steam_blast_furnace", "bronze", true},
		{"steam_blast_furnace", "steel", true},
		{"steam_blast_furnace", "electric", false},
	}
	for _, tc := range tests {
		t.Run(tc.machineID+"/"+tc.tier, func(t *testing.T) {
			vs, err := miProgram(t).Evaluate(context.Background(), macerator(tc.machineID, tc.tier))
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if vs[0].Valid != tc.want {
				t.Errorf("valid = %v, want %v", vs[0].Valid, tc.want)
			}
		})
	}
}

// Same rate and same EU, so only rank can say which coil MI would rather see.
func TestMIRanksCupronickelFirst(t *testing.T) {
	ebf := func(machineID string) EvalContext {
		return EvalContext{
			Machine: EvalMachine{
				ModID: "modern_industrialization", MachineID: machineID,
				Data: json.RawMessage(`{"base_energy_per_tick":128,"max_energy_per_tick":128}`),
			},
			Recipe: EvalRecipe{ID: "r", DurationTicks: 100, Data: json.RawMessage(`{"energy_per_tick":32}`)},
			Config: json.RawMessage(`{"tier":"electric"}`),
		}
	}
	prog := miProgram(t)
	cupro, err := prog.Evaluate(context.Background(), ebf("electric_blast_furnace_cupronickel"))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	kanthal, err := prog.Evaluate(context.Background(), ebf("electric_blast_furnace"))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if cupro[0].Rank >= kanthal[0].Rank {
		t.Errorf("cupronickel rank %d, kanthal rank %d; want cupronickel lower", cupro[0].Rank, kanthal[0].Rank)
	}
}
