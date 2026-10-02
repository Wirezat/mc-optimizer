package solver

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

func sp(s string) *string { return &s }

// stubStore is a minimal in-memory RecipeStore for unit tests.
type stubStore struct {
	recipes    map[string]*RecipeRow
	byItem     map[string][]*RecipeRow
	machines   map[string]*MachineSpec
	interfaces map[string][]MachineRef // recipe id → machine_interfaces implementers
	tags       map[string][]resource.Ref
	byFluid    map[string][]*RecipeRow
}

// GetRecipesFor mirrors the real query: one row per (recipe, machine), the recipe's own
// machine first, then the machine_interfaces implementers.
func (s *stubStore) GetRecipesFor(_ context.Context, ref resource.Ref) ([]*RecipeRow, error) {
	if ref.Kind.Or() == resource.KindFluid {
		return s.byFluid[ref.ModID+":"+ref.ID], nil
	}
	var out []*RecipeRow
	for _, r := range s.byItem[ref.ModID+":"+ref.ID] {
		out = append(out, r)
		for _, ref := range s.interfaces[r.ID] {
			clone := *r
			clone.MachineMod, clone.MachineID = ref.ModID, ref.MachineID
			out = append(out, &clone)
		}
	}
	return out, nil
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

// GetMachinesForRecipe: the recipe's own machine plus the fixture's interfaces.
func (s *stubStore) GetMachinesForRecipe(_ context.Context, recipeID string) ([]MachineRef, error) {
	r, ok := s.recipes[recipeID]
	if !ok {
		return nil, nil
	}
	return append([]MachineRef{{ModID: r.MachineMod, MachineID: r.MachineID}}, s.interfaces[recipeID]...), nil
}

func (s *stubStore) GetTagMembers(_ context.Context, tag resource.Ref) ([]resource.Ref, error) {
	return s.tags[tag.Key()], nil
}

// ironIngotRecipe is 1 iron ore → 1 iron ingot in a furnace, taking durationTicks.
func ironIngotRecipe(durationTicks int) *RecipeRow {
	return &RecipeRow{
		ID:            "recipe:iron_ingot",
		MachineMod:    "minecraft",
		MachineID:     "furnace",
		DurationTicks: durationTicks,
		Inputs:        []resource.IO{itemIO("minecraft", "iron_ore", 1, 1)},
		Outputs:       []resource.IO{itemIO("minecraft", "iron_ingot", 1, 1)},
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

// calcGroup runs CalculateMachineGroups for the iron-ingot fixture at the given recipe rate
// (recipes per tick) and returns the single group it produces.
func calcGroup(t *testing.T, durationTicks int, recipeRate resource.Rational) MachineGroupDraft {
	t.Helper()
	s := NewSolver(newStub(durationTicks), 1000)
	ctx := context.Background()
	g, err := s.BuildRecipeGraph(ctx, resource.Ref{ModID: "minecraft", ID: "iron_ingot"},
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

// newVariantTestSolver builds a solver over the iron-ingot fixture (a 20-tick recipe on a
// furnace) with the given variant source, plus the matching graph and a rate vector
// demanding 1/10 recipes per tick.
func newVariantTestSolver(t *testing.T, src VariantSource) (*Solver, *RecipeGraph, RateVector) {
	t.Helper()
	s := NewSolver(newStub(20), 1000)
	s.VariantSource = src
	g, err := s.BuildRecipeGraph(context.Background(), resource.Ref{ModID: "minecraft", ID: "iron_ingot"},
		map[string]bool{"minecraft:iron_ore": true}, FactoryState{},
		map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	rv := newRateVector()
	rv.RecipeRates[RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace")] = resource.NewRational(1, 10)
	return s, g, rv
}

// wantRational fails unless r equals num/den exactly.
func wantRational(t *testing.T, label string, r resource.Rational, num, den int64) {
	t.Helper()
	if r.Num*den != num*r.Den {
		t.Errorf("%s = %d/%d, want %d/%d", label, r.Num, r.Den, num, den)
	}
}

// TestCalculateMachineGroups_fractionalCount covers the ceiling and the utilisation the
// rewrite computes: 1/60 recipes per tick on a 90-tick recipe needs 3/2 machines, so two
// machines stand and each runs at 3/4 load.
func TestCalculateMachineGroups_fractionalCount(t *testing.T) {
	g := calcGroup(t, 90, resource.NewRational(1, 60))

	wantRational(t, "ExactCount", g.ExactCount, 3, 2)
	if g.Count != 2 {
		t.Errorf("Count = %d, want 2", g.Count)
	}
	wantRational(t, "Utilization", g.Utilization, 3, 4)
}

// TestCalculateMachineGroups_zeroDurationClampsToOneTick covers max(DurationTicks, 1).
func TestCalculateMachineGroups_zeroDurationClampsToOneTick(t *testing.T) {
	g := calcGroup(t, 0, resource.NewRational(1, 4))

	wantRational(t, "ExactCount", g.ExactCount, 1, 4)
	if g.Count != 1 {
		t.Errorf("Count = %d, want 1", g.Count)
	}
	wantRational(t, "Utilization", g.Utilization, 1, 4)
}

// TestCalculateMachineGroups_wholeCount is the exact-fit case: 1/20 recipes per tick on a
// 20-tick recipe is precisely one fully loaded machine, no rounding involved.
func TestCalculateMachineGroups_wholeCount(t *testing.T) {
	g := calcGroup(t, 20, resource.NewRational(1, 20))

	wantRational(t, "ExactCount", g.ExactCount, 1, 1)
	if g.Count != 1 {
		t.Errorf("Count = %d, want 1", g.Count)
	}
	wantRational(t, "Utilization", g.Utilization, 1, 1)
}

func TestRateVector_itemRates(t *testing.T) {
	rv := newRateVector()
	rv.RecipeRates["recipe:iron_ingot"] = resource.NewRational(1, 20)
	rv.ItemRates["minecraft:iron_ingot"] = resource.NewRational(1, 20)

	if rv.RecipeRates["recipe:iron_ingot"].Den != 20 {
		t.Errorf("unexpected rate: %v", rv.RecipeRates["recipe:iron_ingot"])
	}
}

// A recipe's own machine must be pinnable.
func TestCalculateMachineGroups_PinsTheRecipesOwnMachine(t *testing.T) {
	stub := newStub(200)
	stub.interfaces = map[string][]MachineRef{
		"recipe:iron_ingot": {{ModID: "addon", MachineID: "super_furnace"}},
	}
	stub.machines["addon:super_furnace"] = &MachineSpec{ModID: "addon", MachineID: "super_furnace", Name: "Super Furnace"}

	run := func(t *testing.T, override string) MachineGroupDraft {
		t.Helper()
		s := NewSolver(stub, 1000)
		ctx := context.Background()
		overrides := map[string]string{}
		if override != "" {
			overrides["minecraft:iron_ingot"] = override
		}
		g, err := s.BuildRecipeGraph(ctx, resource.Ref{ModID: "minecraft", ID: "iron_ingot"},
			map[string]bool{"minecraft:iron_ore": true}, FactoryState{}, overrides, map[string]string{})
		if err != nil {
			t.Fatalf("BuildRecipeGraph: %v", err)
		}
		rv := newRateVector()
		for key := range g.Nodes {
			node := g.Nodes[key]
			if node.RecipeID == "" {
				continue
			}
			rv.RecipeRates[RecipeOptionKey(node.RecipeID, node.MachineMod, node.MachineID)] = resource.NewRational(1, 100)
		}
		groups, _, err := s.CalculateMachineGroups(ctx, g, rv, SolveRequest{RecipeOverrides: overrides})
		if err != nil {
			t.Fatalf("CalculateMachineGroups: %v", err)
		}
		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}
		return groups[0]
	}

	ownKey := RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace")
	if got := run(t, ownKey); got.MachineID != "furnace" {
		t.Errorf("pinned the recipe's own machine, solver ran %s:%s", got.MachineMod, got.MachineID)
	}

	addonKey := RecipeOptionKey("recipe:iron_ingot", "addon", "super_furnace")
	if got := run(t, addonKey); got.MachineID != "super_furnace" {
		t.Errorf("pinned the implementer, solver ran %s:%s", got.MachineMod, got.MachineID)
	}
}

// Choosing a recipe without naming a machine leaves the machine to the solver: the override
// is the bare recipe id.
func TestCalculateMachineGroups_BareRecipeOverrideLeavesTheMachineOpen(t *testing.T) {
	stub := newStub(200)
	stub.interfaces = map[string][]MachineRef{
		"recipe:iron_ingot": {{ModID: "addon", MachineID: "super_furnace"}},
	}
	stub.machines["addon:super_furnace"] = &MachineSpec{ModID: "addon", MachineID: "super_furnace", Name: "Super Furnace"}

	s := NewSolver(stub, 1000)
	ctx := context.Background()
	overrides := map[string]string{"minecraft:iron_ingot": "recipe:iron_ingot"}

	g, err := s.BuildRecipeGraph(ctx, resource.Ref{ModID: "minecraft", ID: "iron_ingot"},
		map[string]bool{"minecraft:iron_ore": true}, FactoryState{}, overrides, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes["minecraft:iron_ingot"]
	if node.RecipeID != "recipe:iron_ingot" {
		t.Fatalf("bare recipe override did not select the recipe: %+v", node)
	}
}
