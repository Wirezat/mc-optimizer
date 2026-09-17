package solver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

// byMachineSource answers per machine, so a matrix can hold different speeds.
type byMachineSource struct{ vs map[string][]plugins.Variant }

func (s *byMachineSource) Variants(_ context.Context, m *MachineSpec, r *RecipeRow, _ json.RawMessage) ([]plugins.Variant, error) {
	if vs, ok := s.vs[m.ModID+":"+m.MachineID]; ok {
		return vs, nil
	}
	return []plugins.Variant{DefaultVariant(r)}, nil
}

// interfaceStub adds a machine of another mod reaching the same recipe.
func interfaceStub(durationTicks int) *stubStore {
	st := newStub(durationTicks)
	st.machines["ironfurnaces:gold_furnace"] = &MachineSpec{
		ModID: "ironfurnaces", MachineID: "gold_furnace", Name: "Golden Furnace"}
	st.interfaces = map[string][]MachineRef{
		"recipe:iron_ingot": {{ModID: "ironfurnaces", MachineID: "gold_furnace"}},
	}
	return st
}

// matrixGroup solves the iron-ingot node on nodeMachine and returns its group.
func matrixGroup(t *testing.T, st *stubStore, src VariantSource, nodeMachine MachineRef, recipeRate Rational) MachineGroupDraft {
	t.Helper()
	return matrixGroupFor(t, st, src, "recipe:iron_ingot", nodeMachine, recipeRate)
}

func matrixGroupFor(t *testing.T, st *stubStore, src VariantSource, recipeID string, nodeMachine MachineRef, recipeRate Rational) MachineGroupDraft {
	t.Helper()
	s := NewSolver(st, 1000)
	s.VariantSource = src
	ctx := context.Background()
	nodeKey := RecipeOptionKey(recipeID, nodeMachine.ModID, nodeMachine.MachineID)
	// Naming a machine is what pins it. A node on the recipe's own machine is
	// the default the ladder may still improve on, so it overrides by recipe.
	override := recipeID
	if own := st.recipes[recipeID]; own == nil ||
		own.MachineMod != nodeMachine.ModID || own.MachineID != nodeMachine.MachineID {
		override = nodeKey
	}
	overrides := map[string]string{"minecraft:iron_ingot": override}
	g, err := s.BuildRecipeGraph(ctx, ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		map[string]bool{"minecraft:iron_ore": true}, FactoryState{},
		overrides, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes["minecraft:iron_ingot"]
	if node == nil || node.RateKey() != nodeKey {
		t.Fatalf("node = %+v, want one on %s", node, nodeKey)
	}
	rv := newRateVector()
	rv.RecipeRates[nodeKey] = recipeRate
	groups, _, err := s.CalculateMachineGroups(ctx, g, rv, SolveRequest{RecipeOverrides: overrides})
	if err != nil {
		t.Fatalf("CalculateMachineGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	return groups[0]
}

func TestMatrixPicksTheBetterMachineOfAnotherMod(t *testing.T) {
	src := &byMachineSource{vs: map[string][]plugins.Variant{
		"minecraft:furnace":         {{ID: "plain", Rate: rate(1, 200), Valid: true}},
		"ironfurnaces:gold_furnace": {{ID: "plain", Rate: rate(1, 100), Valid: true}},
	}}
	// At 1/100 per tick the vanilla furnace needs two, the gold one.
	g := matrixGroup(t, interfaceStub(200), src, MachineRef{"minecraft", "furnace"}, NewRational(1, 100))
	if g.MachineMod != "ironfurnaces" || g.MachineID != "gold_furnace" {
		t.Errorf("machine = %s:%s, want ironfurnaces:gold_furnace", g.MachineMod, g.MachineID)
	}
	if g.Count != 1 {
		t.Errorf("count = %d, want 1", g.Count)
	}
	if g.RateKey != RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace") {
		t.Errorf("rateKey = %q, want the node's own key", g.RateKey)
	}
	if len(g.VariantOptions) != 1 || g.VariantOptions[0].ID != "plain" {
		t.Errorf("VariantOptions = %+v, want the gold furnace's own list", g.VariantOptions)
	}
}

// The vanilla furnace would win on speed here.
func TestMatrixKeepsAPinnedMachine(t *testing.T) {
	src := &byMachineSource{vs: map[string][]plugins.Variant{
		"minecraft:furnace":         {{ID: "plain", Rate: rate(1, 10), Valid: true}},
		"ironfurnaces:gold_furnace": {{ID: "plain", Rate: rate(1, 200), Valid: true}},
	}}
	g := matrixGroup(t, interfaceStub(200), src, MachineRef{"ironfurnaces", "gold_furnace"}, NewRational(1, 100))
	if g.MachineMod != "ironfurnaces" || g.MachineID != "gold_furnace" {
		t.Errorf("machine = %s:%s, want the pinned ironfurnaces:gold_furnace", g.MachineMod, g.MachineID)
	}
}

func TestMatrixSkipsInactiveMods(t *testing.T) {
	src := &byMachineSource{vs: map[string][]plugins.Variant{
		"minecraft:furnace":         {{ID: "plain", Rate: rate(1, 200), Valid: true}},
		"ironfurnaces:gold_furnace": {{ID: "plain", Rate: rate(1, 100), Valid: true}},
	}}
	s := NewSolver(interfaceStub(200), 1000)
	s.VariantSource = src
	s.ActiveMods = map[string]bool{"minecraft": true}
	ctx := context.Background()
	g, err := s.BuildRecipeGraph(ctx, ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		map[string]bool{"minecraft:iron_ore": true}, FactoryState{},
		map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	rv := newRateVector()
	rv.RecipeRates[RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace")] = NewRational(1, 100)
	groups, _, err := s.CalculateMachineGroups(ctx, g, rv, SolveRequest{})
	if err != nil {
		t.Fatalf("CalculateMachineGroups: %v", err)
	}
	if groups[0].MachineMod != "minecraft" {
		t.Errorf("machine mod = %q, want minecraft: ironfurnaces is not active for this save", groups[0].MachineMod)
	}
}

// Nodes are visited through a map, which Go randomizes; with one pass instead
// of two these runs would disagree.
func TestMatrixPickIsStableAcrossRuns(t *testing.T) {
	src := &byMachineSource{vs: map[string][]plugins.Variant{
		"minecraft:furnace":         {{ID: "plain", Rate: rate(1, 200), Valid: true}},
		"ironfurnaces:gold_furnace": {{ID: "plain", Rate: rate(1, 200), Valid: true}},
	}}
	st := interfaceStub(200)
	var want string
	for i := range 50 {
		g := matrixGroup(t, st, src, MachineRef{"minecraft", "furnace"}, NewRational(1, 100))
		got := g.MachineMod + ":" + g.MachineID + "/" + g.VariantID
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("run %d picked %q, run 0 picked %q", i, got, want)
		}
	}
}

// smeltingSiblings is the same I/O as a 200-tick furnace and 100-tick blasting recipe.
func smeltingSiblings() *stubStore {
	furnace := ironIngotRecipe(200)
	blasting := ironIngotRecipe(100)
	blasting.ID = "recipe:iron_ingot_blasting"
	blasting.MachineID = "blast_furnace"
	st := stubStoreFor([]*RecipeRow{furnace, blasting},
		&MachineSpec{ModID: "minecraft", MachineID: "furnace", Name: "Furnace"},
		&MachineSpec{ModID: "minecraft", MachineID: "blast_furnace", Name: "Blast Furnace"})
	return st
}

// Built on the furnace recipe, the blast furnace still wins.
func TestSiblingRecipesShareOneMatrix(t *testing.T) {
	g := matrixGroup(t, smeltingSiblings(), nil, MachineRef{"minecraft", "furnace"}, NewRational(1, 100))
	if g.RecipeID != "recipe:iron_ingot_blasting" {
		t.Errorf("recipe = %q, want the blasting sibling", g.RecipeID)
	}
	if g.MachineID != "blast_furnace" {
		t.Errorf("machine = %q, want blast_furnace", g.MachineID)
	}
	if g.Count != 1 {
		t.Errorf("count = %d, want 1", g.Count)
	}
}

// Same main product, different byproduct: every downstream rate would be wrong.
func TestRecipesWithDifferentIOAreNotSiblings(t *testing.T) {
	furnace := ironIngotRecipe(200)
	withSlag := ironIngotRecipe(100)
	withSlag.ID = "recipe:iron_ingot_slag"
	withSlag.MachineID = "blast_furnace"
	mc := "minecraft"
	withSlag.ItemOutputs = append(withSlag.ItemOutputs, RecipeRowItemIO{
		ItemModID: &mc, ItemID: sp("slag"), AmountNum: 1, AmountDen: 10,
		ProbabilityNum: 1, ProbabilityDen: 1,
	})
	st := stubStoreFor([]*RecipeRow{furnace, withSlag},
		&MachineSpec{ModID: "minecraft", MachineID: "furnace", Name: "Furnace"},
		&MachineSpec{ModID: "minecraft", MachineID: "blast_furnace", Name: "Blast Furnace"})

	g := matrixGroup(t, st, nil, MachineRef{"minecraft", "furnace"}, NewRational(1, 100))
	if g.RecipeID != "recipe:iron_ingot" {
		t.Errorf("recipe = %q, want the node's own: a byproduct makes it a different recipe", g.RecipeID)
	}
}

// The copper furnace reaches the smelting recipe but not the blasting one.
func TestPinnedMachineOnlyGetsSiblingsItCanRun(t *testing.T) {
	st := smeltingSiblings()
	st.machines["ironfurnaces:copper_furnace"] = &MachineSpec{
		ModID: "ironfurnaces", MachineID: "copper_furnace", Name: "Copper Furnace"}
	st.interfaces = map[string][]MachineRef{
		"recipe:iron_ingot": {{ModID: "ironfurnaces", MachineID: "copper_furnace"}},
	}

	g := matrixGroup(t, st, nil, MachineRef{"ironfurnaces", "copper_furnace"}, NewRational(1, 100))
	if g.MachineMod != "ironfurnaces" || g.MachineID != "copper_furnace" {
		t.Errorf("machine = %s:%s, want the pinned ironfurnaces:copper_furnace", g.MachineMod, g.MachineID)
	}
	if g.RecipeID != "recipe:iron_ingot" {
		t.Errorf("recipe = %q, want the smelting one: the copper furnace cannot run the blasting sibling", g.RecipeID)
	}
}

func TestIOSignatureIsStrict(t *testing.T) {
	base := ironIngotRecipe(200)
	same := ironIngotRecipe(100) // only the duration differs
	if ioSignature(base) != ioSignature(same) {
		t.Error("recipes differing only in duration must share a signature")
	}

	probability := ironIngotRecipe(100)
	probability.ItemOutputs[0].ProbabilityNum, probability.ItemOutputs[0].ProbabilityDen = 9, 10
	if ioSignature(base) == ioSignature(probability) {
		t.Error("a 90% output must not match a certain one")
	}

	amount := ironIngotRecipe(100)
	amount.ItemInputs[0].AmountNum = 2
	if ioSignature(base) == ioSignature(amount) {
		t.Error("a doubled input must not match")
	}

	tool := ironIngotRecipe(100)
	tool.ItemInputs[0].NonConsuming = true
	if ioSignature(base) == ioSignature(tool) {
		t.Error("an input that survives the craft must not match one that is consumed")
	}
}

// pulverizerCells is Thermal's case: an output augment raises the nickel
// byproduct from 1/10 to 19/100 for 3.8x the energy, at the same rate.
func pulverizerCells() []cell {
	mc := "minecraft"
	recipe := &RecipeRow{
		ID: "r:pulverize", MachineMod: "thermal", MachineID: "pulverizer", DurationTicks: 100,
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: sp("iron_dust"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
			{ItemModID: &mc, ItemID: sp("nickel"), AmountNum: 1, AmountDen: 10, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
	outs := func(nickelNum, nickelDen int64) []plugins.Output {
		return []plugins.Output{
			{Ref: "minecraft:iron_dust", Amount: rate(1, 1), Probability: rate(1, 1)},
			{Ref: "minecraft:nickel", Amount: rate(nickelNum, nickelDen), Probability: rate(1, 1)},
		}
	}
	machine := &MachineSpec{ModID: "thermal", MachineID: "pulverizer"}
	return []cell{
		{machine: machine, recipe: recipe, variant: plugins.Variant{
			ID: "plain", Rate: rate(1, 100), Valid: true, Outputs: outs(1, 10),
			Costs: []plugins.Cost{{Resource: "rf", Amount: rate(20, 1)}}}},
		{machine: machine, recipe: recipe, variant: plugins.Variant{
			ID: "boosted", Rate: rate(1, 100), Valid: true, Outputs: outs(19, 100),
			Costs: []plugins.Cost{{Resource: "rf", Amount: rate(76, 1)}}}},
	}
}

// One machine at the nickel node makes 1/100 nickel per tick.
func nickelYields() yieldIndex {
	return yieldIndex{"minecraft:nickel": NewRational(1, 100)}
}

func TestByproductValueOnlyWinsByAWholeMachine(t *testing.T) {
	tests := []struct {
		name   string
		demand Rational
		yields yieldIndex
		want   string
	}{
		// 0.09 machines saved, so stage 5 decides instead.
		{"one machine", NewRational(1, 100), nickelYields(), "plain"},
		{"a hundred machines", NewRational(1, 1), nickelYields(), "boosted"},
		{"nickel unused", NewRational(1, 1), yieldIndex{}, "plain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pickCell(pulverizerCells(), tc.demand, &ladderCtx{yields: tc.yields})
			if !ok {
				t.Fatal("ok = false, want true")
			}
			if got.variant.ID != tc.want {
				t.Errorf("ID = %q, want %q (saved %v machines)", got.variant.ID, tc.want, got.byproduct)
			}
		})
	}
}

func TestIndexYields(t *testing.T) {
	nickel := ItemRef{ModID: "minecraft", ItemID: "nickel"}
	g := &RecipeGraph{Nodes: map[string]*RecipeNode{
		"minecraft:nickel": {Item: nickel, RecipeID: "r:nickel", OutputAmount: NewRational(2, 1)},
		"minecraft:stone":  {Item: ItemRef{ModID: "minecraft", ItemID: "stone"}},
	}}
	groups := []MachineGroupDraft{
		{RecipeOutput: nickel, Variant: plugins.Variant{Rate: rate(1, 50)}},
		{RecipeOutput: ItemRef{ModID: "minecraft", ItemID: "stone"}, Variant: plugins.Variant{Rate: rate(1, 10)}},
	}
	yi := indexYields(g, groups)
	wantRational(t, "nickel per machine", yi["minecraft:nickel"], 1, 25)
	if _, ok := yi["minecraft:stone"]; ok {
		t.Error("a node without a recipe must not contribute a yield")
	}
}

func vanillaMachine(id string) *MachineSpec {
	return &MachineSpec{ModID: "minecraft", MachineID: id, Ecosystem: VanillaEcosystem}
}

// Fuel burns by time, so the rate changes with the recipe and the coal per tick
// does not: the blast furnace gets sixteen items out of the coal the furnace
// turns into eight.
func TestVanillaVariantChargesFuelPerTick(t *testing.T) {
	for _, tc := range []struct {
		machine string
		ticks   int
	}{
		{"furnace", 200},
		{"blast_furnace", 100},
		{"smoker", 100},
	} {
		t.Run(tc.machine, func(t *testing.T) {
			v := vanillaVariant(vanillaMachine(tc.machine), ironIngotRecipe(tc.ticks), nil)
			if !v.Valid || v.Rate.Num != 1 || v.Rate.Den != int64(tc.ticks) {
				t.Fatalf("variant = %+v, want a valid 1/%d rate", v, tc.ticks)
			}
			if len(v.Costs) != 1 || v.Costs[0].Resource != "coals" ||
				v.Costs[0].Amount.Num != 1 || v.Costs[0].Amount.Den != 1600 {
				t.Errorf("costs = %+v, want 1/1600 coals whatever the duration", v.Costs)
			}
		})
	}
}

func TestVanillaVariantLeavesFuellessMachinesFree(t *testing.T) {
	v := vanillaVariant(vanillaMachine("stonecutter"), ironIngotRecipe(1), nil)
	if len(v.Costs) != 0 || !v.Valid {
		t.Errorf("variant = %+v, want a valid free one: a stonecutter burns nothing", v)
	}
}

func TestVanillaVariantStandsDownForAPinnedSupply(t *testing.T) {
	cfg := map[string]json.RawMessage{"ironfurnaces": json.RawMessage(`{"mode":"factory"}`)}
	if v := vanillaVariant(vanillaMachine("furnace"), ironIngotRecipe(200), cfg); v.Valid {
		t.Error("valid = true, want false while Iron Furnaces runs on RF")
	}
	cfg["ironfurnaces"] = json.RawMessage(`{"mode":"none"}`)
	if v := vanillaVariant(vanillaMachine("furnace"), ironIngotRecipe(200), cfg); !v.Valid {
		t.Error("valid = false, want true without a factory pin")
	}
}

func TestVanillaSkipsTheVariantSource(t *testing.T) {
	src := &stubSource{vs: []plugins.Variant{{ID: "fromPlugin", Rate: rate(1, 1), Valid: true}}}
	st := newStub(200)
	st.machines["minecraft:furnace"].Ecosystem = VanillaEcosystem
	g := matrixGroup(t, st, src, MachineRef{"minecraft", "furnace"}, NewRational(1, 200))
	if g.VariantID != "vanilla" {
		t.Errorf("variant = %q, want the host's own", g.VariantID)
	}
	if src.calls != 0 {
		t.Errorf("VariantSource called %d times, want 0", src.calls)
	}
}
