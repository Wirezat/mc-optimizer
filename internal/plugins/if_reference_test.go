package plugins

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func ifProgram(t *testing.T) *Program {
	t.Helper()
	src, err := os.ReadFile("../../mod-plugins/ironfurnaces/plugin/plugin.js")
	if err != nil {
		t.Fatalf("read IF plugin: %v", err)
	}
	prog, err := Compile("ironfurnaces", string(src))
	if err != nil {
		t.Fatalf("compile IF plugin: %v", err)
	}
	return prog
}

// furnaceContext is one of the vanilla recipe types on one Iron Furnace. The
// machine data is what the importer writes from Config.java's defaults.
func furnaceContext(machineID string, speed, tier, batch int, baseMachine string, ticks int64, config string) EvalContext {
	return EvalContext{
		Machine: EvalMachine{
			ModID: "ironfurnaces", MachineID: machineID,
			Data: json.RawMessage(`{"speed":` + itoa(speed) + `,"energy_tier":` + itoa(tier) +
				`,"batch":` + itoa(batch) + `,"generation":40}`),
		},
		Recipe: EvalRecipe{
			ID: "r:smelt", MachineMod: "minecraft", MachineID: baseMachine, DurationTicks: ticks,
			Outputs: []Output{{Ref: "minecraft:iron_ingot", Amount: Rational{1, 1}, Probability: Rational{1, 1}}},
			Data:    json.RawMessage(`{}`),
		},
		Config: json.RawMessage(config),
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func variantByID(t *testing.T, vs []Variant, id string) Variant {
	t.Helper()
	for _, v := range vs {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("no variant %q among %d", id, len(vs))
	return Variant{}
}

const goldConfig = `{"max_tier":"gold_furnace","mode":"none","fuel":"coals"}`

// The golden furnace on a 200-tick smelting recipe: three operating points. The
// coal per tick is the rate times the mod's own per-item figures - 1/8 plain,
// 1/16 with fuel efficiency, 1/4 with speed - in the unit the host charges
// vanilla in, so the two are comparable in the ladder.
func TestIFGoldOnASmeltingRecipe(t *testing.T) {
	vs, err := ifProgram(t).Evaluate(context.Background(),
		furnaceContext("gold_furnace", 120, 1, 1, "furnace", 200, goldConfig))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(vs) != 3 {
		t.Fatalf("got %d variants, want 3", len(vs))
	}
	for _, tc := range []struct {
		id        string
		num, den  int64
		coalDen   int64
		itemCount int
	}{
		{"plain", 1, 120, 960, 0},
		{"fuel", 1, 150, 2400, 1},
		{"speed", 1, 60, 240, 1},
	} {
		v := variantByID(t, vs, tc.id)
		if v.Rate.Num != tc.num || v.Rate.Den != tc.den {
			t.Errorf("%s rate = %d/%d, want %d/%d", tc.id, v.Rate.Num, v.Rate.Den, tc.num, tc.den)
		}
		if len(v.Costs) != 1 || v.Costs[0].Resource != "coals" ||
			v.Costs[0].Amount.Num*tc.coalDen != v.Costs[0].Amount.Den {
			t.Errorf("%s coal = %+v, want 1/%d per tick", tc.id, v.Costs, tc.coalDen)
		}
		if len(v.Items) != tc.itemCount {
			t.Errorf("%s installs %d items, want %d", tc.id, len(v.Items), tc.itemCount)
		}
	}
}

// A blasting recipe halves the ticks and puts the red augment in every cell, so
// this machine has no base variant at all - the case that made "exactly one"
// impossible to satisfy.
func TestIFBlastingCarriesTheRedAugmentInEveryCell(t *testing.T) {
	vs, err := ifProgram(t).Evaluate(context.Background(),
		furnaceContext("gold_furnace", 120, 1, 1, "blast_furnace", 100, goldConfig))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(vs) != 3 {
		t.Fatalf("got %d variants, want 3", len(vs))
	}
	for _, v := range vs {
		if len(v.Items) == 0 {
			t.Errorf("variant %q has no items; a blasting recipe needs the augment", v.ID)
		}
	}
	if v := variantByID(t, vs, "blasting_plain"); v.Rate.Num != 1 || v.Rate.Den != 60 {
		t.Errorf("blasting_plain rate = %d/%d, want 1/60", v.Rate.Num, v.Rate.Den)
	}
}

// Factory mode trades the fuel for RF and runs the tier's slots in parallel.
func TestIFFactoryModeRunsParallelOnRF(t *testing.T) {
	cfg := `{"max_tier":"gold_furnace","mode":"factory"}`
	vs, err := ifProgram(t).Evaluate(context.Background(),
		furnaceContext("gold_furnace", 120, 1, 1, "furnace", 200, cfg))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	v := variantByID(t, vs, "factory_plain")
	// Tier 1 is four slots, so four times the single furnace's 1/120.
	if v.Rate.Num != 4 || v.Rate.Den != 120 {
		t.Errorf("rate = %d/%d, want 4/120", v.Rate.Num, v.Rate.Den)
	}
	if len(v.Costs) != 1 || v.Costs[0].Resource != "rf" {
		t.Errorf("costs = %+v, want rf", v.Costs)
	}
	if v.Costs[0].Amount.Num != 4*200*20 || v.Costs[0].Amount.Den != 120 {
		t.Errorf("rf = %+v, want four slots at 4000 RF per craft over 120 ticks", v.Costs[0].Amount)
	}
}

// The config is a ceiling over one supply, so a furnace above it is vetoed.
func TestIFVetoesMaterialAboveTheConfig(t *testing.T) {
	vs, err := ifProgram(t).Evaluate(context.Background(),
		furnaceContext("emerald_furnace", 40, 2, 1, "furnace", 200, goldConfig))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(vs) != 1 || vs[0].Valid {
		t.Fatalf("got %+v, want one invalid cell", vs)
	}
}

// Unobtainium runs a craft in one tick, so speed cannot shorten it and is
// dropped rather than offered at twice the fuel.
func TestIFUnobtainiumDropsThePointlessSpeedCell(t *testing.T) {
	cfg := `{"max_tier":"unobtainium_furnace","mode":"none","fuel":"coal_block"}`
	vs, err := ifProgram(t).Evaluate(context.Background(),
		furnaceContext("unobtainium_furnace", 1, 2, 64, "furnace", 200, cfg))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(vs) != 2 {
		t.Fatalf("got %d variants, want 2 (plain and fuel)", len(vs))
	}
	v := variantByID(t, vs, "plain")
	if v.Rate.Num != 64 || v.Rate.Den != 1 {
		t.Errorf("rate = %d/%d, want 64/1", v.Rate.Num, v.Rate.Den)
	}
	// 16000 burn over 64 items a craft is one coal block per 5120 items, and at
	// 64 items a tick that is 1/80 of a block per tick.
	if c := v.Costs[0]; c.Resource != "coal_block" || c.Amount.Num*80 != c.Amount.Den {
		t.Errorf("fuel = %+v, want 1/80 coal blocks per tick", c)
	}
}

func TestIFGeneratorModeSmeltsNothing(t *testing.T) {
	cfg := `{"max_tier":"gold_furnace","mode":"generator"}`
	vs, err := ifProgram(t).Evaluate(context.Background(),
		furnaceContext("gold_furnace", 120, 1, 1, "furnace", 200, cfg))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(vs) != 1 || vs[0].Valid {
		t.Fatalf("got %+v, want one invalid cell", vs)
	}
}
