package solver

import (
	"context"
	"testing"
)

func sp(s string) *string { return &s }

// stubStore is a minimal in-memory RecipeStore for unit tests.
type stubStore struct {
	recipes  map[string]*RecipeRow
	byItem   map[string][]*RecipeRow
	machines map[string]*MachineSpec
}

func (s *stubStore) GetRecipesForItem(_ context.Context, modID, itemID string) ([]*RecipeRow, error) {
	return s.byItem[modID+":"+itemID], nil
}

func (s *stubStore) GetRecipesForFluid(_ context.Context, _, _ string) ([]*RecipeRow, error) {
	return nil, nil
}

func (s *stubStore) GetRecipe(_ context.Context, id string) (*RecipeRow, error) {
	r, ok := s.recipes[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (s *stubStore) GetMachineType(_ context.Context, modID, machineID string) (*MachineSpec, error) {
	return s.machines[modID+":"+machineID], nil
}

func (s *stubStore) GetTagMembers(_ context.Context, _ string) ([]ItemRef, error) {
	return nil, nil
}

// ironIngotRecipe is 1 iron ore → 1 iron ingot in a furnace, taking durationTicks.
func ironIngotRecipe(durationTicks int) *RecipeRow {
	iron := "iron_ore"
	mc := "minecraft"
	return &RecipeRow{
		ID:            "recipe:iron_ingot",
		MachineMod:    "minecraft",
		MachineID:     "furnace",
		DurationTicks: durationTicks,
		ItemInputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: &iron, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: sp("iron_ingot"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
}

func newStub(durationTicks int) *stubStore {
	recipe := ironIngotRecipe(durationTicks)
	return &stubStore{
		recipes: map[string]*RecipeRow{"recipe:iron_ingot": recipe},
		byItem:  map[string][]*RecipeRow{"minecraft:iron_ingot": {recipe}},
		machines: map[string]*MachineSpec{
			"minecraft:furnace": {ModID: "minecraft", MachineID: "furnace", Name: "Furnace"},
		},
	}
}

// calcGroup runs CalculateMachineGroups for the iron-ingot fixture at the given
// recipe rate (recipes per tick) and returns the single group it produces.
func calcGroup(t *testing.T, durationTicks int, recipeRate Rational) MachineGroupDraft {
	t.Helper()
	s := NewSolver(newStub(durationTicks), 1000)
	ctx := context.Background()
	g, err := s.BuildRecipeGraph(ctx, ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		map[string]bool{"minecraft:iron_ore": true}, FactoryState{},
		map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	rv := newRateVector()
	rv.RecipeRates[RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace")] = recipeRate
	groups, _, err := s.CalculateMachineGroups(ctx, g, rv, SolveRequest{})
	if err != nil {
		t.Fatalf("CalculateMachineGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 machine group, got %d", len(groups))
	}
	return groups[0]
}

// newVariantTestSolver builds a solver over the iron-ingot fixture (a 20-tick
// recipe on a furnace) with the given variant source, plus the matching graph and
// a rate vector demanding 1/10 recipes per tick.
func newVariantTestSolver(t *testing.T, src VariantSource) (*Solver, *RecipeGraph, RateVector) {
	t.Helper()
	s := NewSolver(newStub(20), 1000)
	s.VariantSource = src
	g, err := s.BuildRecipeGraph(context.Background(), ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		map[string]bool{"minecraft:iron_ore": true}, FactoryState{},
		map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	rv := newRateVector()
	rv.RecipeRates[RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace")] = NewRational(1, 10)
	return s, g, rv
}

// wantRational fails unless r equals num/den exactly.
func wantRational(t *testing.T, label string, r Rational, num, den int64) {
	t.Helper()
	if r.Num*den != num*r.Den {
		t.Errorf("%s = %d/%d, want %d/%d", label, r.Num, r.Den, num, den)
	}
}

// TestCalculateMachineGroups_fractionalCount covers the ceiling and the utilisation the
// rewrite computes: 1/60 recipes per tick on a 90-tick recipe needs 3/2 machines, so two
// machines stand and each runs at 3/4 load. A count that merely truncated, or a
// utilisation left at 1, would both fail here.
func TestCalculateMachineGroups_fractionalCount(t *testing.T) {
	g := calcGroup(t, 90, NewRational(1, 60))

	wantRational(t, "ExactCount", g.ExactCount, 3, 2)
	if g.Count != 2 {
		t.Errorf("Count = %d, want 2", g.Count)
	}
	wantRational(t, "Utilization", g.Utilization, 3, 4)
}

// TestCalculateMachineGroups_zeroDurationClampsToOneTick covers max(DurationTicks, 1).
// A recipe row with duration 0 must be costed as a single tick: at 1/4 recipes per tick
// that is 1/4 of a machine. Without the clamp ExactCount would collapse to 0 and the
// utilisation would report an idle machine as fully loaded.
func TestCalculateMachineGroups_zeroDurationClampsToOneTick(t *testing.T) {
	g := calcGroup(t, 0, NewRational(1, 4))

	wantRational(t, "ExactCount", g.ExactCount, 1, 4)
	if g.Count != 1 {
		t.Errorf("Count = %d, want 1", g.Count)
	}
	wantRational(t, "Utilization", g.Utilization, 1, 4)
}

// TestCalculateMachineGroups_wholeCount is the exact-fit case: 1/20 recipes per tick on a
// 20-tick recipe is precisely one fully loaded machine, no rounding involved.
func TestCalculateMachineGroups_wholeCount(t *testing.T) {
	g := calcGroup(t, 20, NewRational(1, 20))

	wantRational(t, "ExactCount", g.ExactCount, 1, 1)
	if g.Count != 1 {
		t.Errorf("Count = %d, want 1", g.Count)
	}
	wantRational(t, "Utilization", g.Utilization, 1, 1)
}

func TestRateVector_itemRates(t *testing.T) {
	rv := newRateVector()
	rv.RecipeRates["recipe:iron_ingot"] = NewRational(1, 20)
	rv.ItemRates["minecraft:iron_ingot"] = NewRational(1, 20)

	if rv.RecipeRates["recipe:iron_ingot"].Den != 20 {
		t.Errorf("unexpected rate: %v", rv.RecipeRates["recipe:iron_ingot"])
	}
}
