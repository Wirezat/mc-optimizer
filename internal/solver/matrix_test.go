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

// matrixGroup solves the iron-ingot node at recipeRate for the given node
// machine and returns the one group it produces.
func matrixGroup(t *testing.T, st *stubStore, src VariantSource, nodeMachine MachineRef, recipeRate Rational) MachineGroupDraft {
	t.Helper()
	s := NewSolver(st, 1000)
	s.VariantSource = src
	ctx := context.Background()
	g, err := s.BuildRecipeGraph(ctx, ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		map[string]bool{"minecraft:iron_ore": true}, FactoryState{},
		map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	rv := newRateVector()
	rv.RecipeRates[RecipeOptionKey("recipe:iron_ingot", nodeMachine.ModID, nodeMachine.MachineID)] = recipeRate
	groups, _, err := s.CalculateMachineGroups(ctx, g, rv, SolveRequest{})
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
	if g.rateKey != RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace") {
		t.Errorf("rateKey = %q, want the node's own key", g.rateKey)
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
