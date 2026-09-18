package solver

import (
	"context"
	"testing"
)

// An override naming a machine that reaches the recipe through machine_interfaces must
// select that machine.
func TestBuildRecipeGraphOverrideSelectsInterfaceMachine(t *testing.T) {
	mod := "m"
	widget := "widget"

	// Two candidates for one recipe, base first, exactly the shape GetRecipesForItem now
	// returns.
	base := &RecipeRow{
		ID: "r1", MachineMod: "m", MachineID: "base_machine", DurationTicks: 1,
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: &mod, ItemID: &widget, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
	tier := &RecipeRow{
		ID: "r1", MachineMod: "m", MachineID: "tier_machine", DurationTicks: 1,
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: &mod, ItemID: &widget, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}

	target := ItemRef{ModID: "m", ItemID: "widget"}
	store := &stubStore{
		byItem: map[string][]*RecipeRow{"m:widget": {base, tier}},
	}
	s := NewSolver(store, 1000)

	want := RecipeOptionKey("r1", "m", "tier_machine")
	g, err := s.BuildRecipeGraph(context.Background(), target, nil, FactoryState{},
		map[string]string{target.Key(): want}, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes[target.Key()]
	if node == nil {
		t.Fatal("no node for the target item")
	}
	if node.IsRawMaterial {
		t.Fatal("node fell back to raw material; the override did not match any candidate")
	}
	if node.MachineID != "tier_machine" {
		t.Errorf("node.MachineID = %q, want \"tier_machine\"", node.MachineID)
	}
}

// interfaceCandidates builds the two-candidate fixture BuildRecipeGraph now sees for one
// recipe reachable through machine_interfaces: a base machine owned by baseMod and an
// implementer owned by tierMod, base sorted first as GetRecipesForItem does.
func interfaceCandidates(baseMod, tierMod string) (base, tier *RecipeRow) {
	mod := "m"
	widget := "widget"
	out := []RecipeRowItemIO{
		{ItemModID: &mod, ItemID: &widget, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
	}
	base = &RecipeRow{ID: "r1", MachineMod: baseMod, MachineID: "base_machine", DurationTicks: 1, ItemOutputs: out}
	tier = &RecipeRow{ID: "r1", MachineMod: tierMod, MachineID: "tier_machine", DurationTicks: 1, ItemOutputs: out}
	return base, tier
}

// If only the base machine's mod is active, filterByActiveMods must drop the implementer
// candidate and leave the base machine as the sole (and therefore default) pick.
func TestBuildRecipeGraphActiveModsBaseSurvivesWhenImplementerInactive(t *testing.T) {
	base, tier := interfaceCandidates("base_mod", "tier_mod")
	target := ItemRef{ModID: "m", ItemID: "widget"}
	store := &stubStore{byItem: map[string][]*RecipeRow{"m:widget": {base, tier}}}
	s := NewSolver(store, 1000)
	s.ActiveMods = map[string]bool{"base_mod": true}

	g, err := s.BuildRecipeGraph(context.Background(), target, nil, FactoryState{}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes[target.Key()]
	if node == nil || node.IsRawMaterial {
		t.Fatal("node missing or raw material; the active base machine should have survived filtering")
	}
	if node.MachineID != "base_machine" {
		t.Errorf("node.MachineID = %q, want \"base_machine\"", node.MachineID)
	}
}

// If only the implementer's mod is active, the base machine must be filtered out but the
// recipe must still resolve through the implementer - not collapse to a raw material.
func TestBuildRecipeGraphActiveModsImplementerSurvivesWhenBaseInactive(t *testing.T) {
	base, tier := interfaceCandidates("base_mod", "tier_mod")
	target := ItemRef{ModID: "m", ItemID: "widget"}
	store := &stubStore{byItem: map[string][]*RecipeRow{"m:widget": {base, tier}}}
	s := NewSolver(store, 1000)
	s.ActiveMods = map[string]bool{"tier_mod": true}

	g, err := s.BuildRecipeGraph(context.Background(), target, nil, FactoryState{}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes[target.Key()]
	if node == nil {
		t.Fatal("no node for the target item")
	}
	if node.IsRawMaterial {
		t.Fatal("node fell back to raw material; the active implementer should have kept the recipe resolvable")
	}
	if node.MachineID != "tier_machine" {
		t.Errorf("node.MachineID = %q, want \"tier_machine\"", node.MachineID)
	}
}

func TestBuildRecipeGraphActiveModsBothActiveKeepsBaseDefaultAndImplementerSelectable(t *testing.T) {
	base, tier := interfaceCandidates("base_mod", "tier_mod")
	target := ItemRef{ModID: "m", ItemID: "widget"}
	store := &stubStore{byItem: map[string][]*RecipeRow{"m:widget": {base, tier}}}
	s := NewSolver(store, 1000)
	s.ActiveMods = map[string]bool{"base_mod": true, "tier_mod": true}

	g, err := s.BuildRecipeGraph(context.Background(), target, nil, FactoryState{}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes[target.Key()]
	if node == nil || node.IsRawMaterial {
		t.Fatal("node missing or raw material with both mods active")
	}
	if node.MachineID != "base_machine" {
		t.Errorf("default node.MachineID = %q, want \"base_machine\"", node.MachineID)
	}

	want := RecipeOptionKey("r1", "tier_mod", "tier_machine")
	g2, err := s.BuildRecipeGraph(context.Background(), target, nil, FactoryState{},
		map[string]string{target.Key(): want}, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph with override: %v", err)
	}
	node2 := g2.Nodes[target.Key()]
	if node2 == nil || node2.IsRawMaterial {
		t.Fatal("node missing or raw material; the implementer was filtered out despite its mod being active")
	}
	if node2.MachineID != "tier_machine" {
		t.Errorf("overridden node.MachineID = %q, want \"tier_machine\"", node2.MachineID)
	}
}
