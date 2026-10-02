package solver

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// filterByActiveMods (pure)

func TestFilterByActiveMods_nilMeansUnrestricted(t *testing.T) {
	recipes := []*RecipeRow{{ID: "r1", MachineMod: "modern_industrialization"}}
	got := filterByActiveMods(recipes, nil)
	if len(got) != 1 || got[0] != recipes[0] {
		t.Fatalf("expected recipes unchanged for nil activeMods, got %v", got)
	}
}

func TestFilterByActiveMods_emptyMapRestrictsToNothing(t *testing.T) {
	recipes := []*RecipeRow{{ID: "r1", MachineMod: "modern_industrialization"}}
	got := filterByActiveMods(recipes, map[string]bool{})
	if len(got) != 0 {
		t.Fatalf("expected no recipes for empty activeMods, got %v", got)
	}
}

func TestFilterByActiveMods_keepsOnlyActiveMachineMod(t *testing.T) {
	active := &RecipeRow{ID: "active", MachineMod: "minecraft"}
	inactive := &RecipeRow{ID: "inactive", MachineMod: "powah"}
	got := filterByActiveMods([]*RecipeRow{active, inactive}, map[string]bool{"minecraft": true})
	if len(got) != 1 || got[0].ID != "active" {
		t.Fatalf("expected only the active-mod recipe to survive, got %v", got)
	}
}

// Discover: ModRestricted vs. IsRawMaterial

func modRestrictedStub() *stubStore {
	r := &RecipeRow{
		ID:         "recipe:titanium_plate",
		MachineMod: "powah",
		MachineID:  "press",
		Outputs:    []resource.IO{itemIO("powah", "titanium_plate", 1, 1)},
	}
	return &stubStore{
		recipes: map[string]*RecipeRow{r.ID: r},
		byItem:  map[string][]*RecipeRow{"powah:titanium_plate": {r}},
	}
}

func TestDiscover_modRestricted_whenOwningModInactive(t *testing.T) {
	s := NewSolver(modRestrictedStub(), 0)
	s.ActiveMods = map[string]bool{"minecraft": true} // powah not included

	res, err := s.Discover(context.Background(), resource.Ref{ModID: "powah", ID: "titanium_plate"},
		map[string]bool{}, FactoryState{}, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Items))
	}
	ci := res.Items[0]
	if !ci.ModRestricted {
		t.Error("expected ModRestricted=true when the recipe's mod is inactive")
	}
	if ci.IsRawMaterial {
		t.Error("expected IsRawMaterial=false — a recipe exists, it's just filtered, not absent")
	}
	if !ci.IsStop {
		t.Error("expected IsStop=true — mod-restricted items are dead ends like raw materials")
	}
	if len(ci.Options) != 0 {
		t.Errorf("expected no pickable options, got %d", len(ci.Options))
	}
}

func TestDiscover_normal_whenOwningModActive(t *testing.T) {
	s := NewSolver(modRestrictedStub(), 0)
	s.ActiveMods = map[string]bool{"powah": true}

	res, err := s.Discover(context.Background(), resource.Ref{ModID: "powah", ID: "titanium_plate"},
		map[string]bool{}, FactoryState{}, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	ci := res.Items[0]
	if ci.ModRestricted {
		t.Error("expected ModRestricted=false when the recipe's mod is active")
	}
	if len(ci.Options) != 1 {
		t.Errorf("expected the recipe to be offered as an option, got %d options", len(ci.Options))
	}
}

func TestDiscover_unrestricted_whenActiveModsNil(t *testing.T) {
	s := NewSolver(modRestrictedStub(), 0) // ActiveMods left nil — demo mode

	res, err := s.Discover(context.Background(), resource.Ref{ModID: "powah", ID: "titanium_plate"},
		map[string]bool{}, FactoryState{}, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	ci := res.Items[0]
	if ci.ModRestricted {
		t.Error("expected ModRestricted=false when ActiveMods is nil (unrestricted)")
	}
	if len(ci.Options) != 1 {
		t.Errorf("expected the recipe to be offered as an option, got %d options", len(ci.Options))
	}
}

func TestDiscover_rawMaterial_unaffectedByModFilter(t *testing.T) {
	s := NewSolver(&stubStore{}, 0) // no recipes registered at all
	s.ActiveMods = map[string]bool{"minecraft": true}

	res, err := s.Discover(context.Background(), resource.Ref{ModID: "minecraft", ID: "iron_ore"},
		map[string]bool{}, FactoryState{}, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	ci := res.Items[0]
	if !ci.IsRawMaterial {
		t.Error("expected IsRawMaterial=true — no recipe exists at all, not just filtered")
	}
	if ci.ModRestricted {
		t.Error("expected ModRestricted=false — there was nothing to filter")
	}
}

// BuildRecipeGraph: filtered recipes fall back to IsRawMaterial

func TestBuildRecipeGraph_modRestricted_fallsBackToRawMaterial(t *testing.T) {
	s := NewSolver(modRestrictedStub(), 0)
	s.ActiveMods = map[string]bool{"minecraft": true} // powah not included

	g, err := s.BuildRecipeGraph(context.Background(), resource.Ref{ModID: "powah", ID: "titanium_plate"},
		map[string]bool{}, FactoryState{}, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes["powah:titanium_plate"]
	if node == nil {
		t.Fatal("expected a node for the root item")
	}
	if !node.IsRawMaterial {
		t.Error("expected IsRawMaterial=true when the only recipe's mod is inactive")
	}
	if node.RecipeID != "" {
		t.Errorf("expected no recipe selected, got %q", node.RecipeID)
	}
}

func TestBuildRecipeGraph_activeMod_selectsRecipe(t *testing.T) {
	s := NewSolver(modRestrictedStub(), 0)
	s.ActiveMods = map[string]bool{"powah": true}

	g, err := s.BuildRecipeGraph(context.Background(), resource.Ref{ModID: "powah", ID: "titanium_plate"},
		map[string]bool{}, FactoryState{}, map[string]string{}, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes["powah:titanium_plate"]
	if node == nil || node.RecipeID != "recipe:titanium_plate" {
		t.Fatalf("expected the active-mod recipe to be selected, got node=%+v", node)
	}
}

// A stale recipe override (pointing at a recipe whose mod has since been deactivated) must
// not silently fall through to some other active recipe the user never chose — it must be a
// dead end, same as the fluid branch.
func TestBuildRecipeGraph_staleOverride_fallsBackToRawMaterial(t *testing.T) {
	active := &RecipeRow{ID: "recipe:active", MachineMod: "minecraft", MachineID: "furnace"}
	overridden := &RecipeRow{ID: "recipe:overridden", MachineMod: "powah", MachineID: "press"}
	store := &stubStore{
		recipes: map[string]*RecipeRow{active.ID: active, overridden.ID: overridden},
		byItem:  map[string][]*RecipeRow{"minecraft:iron_plate": {active, overridden}},
	}
	s := NewSolver(store, 0)
	s.ActiveMods = map[string]bool{"minecraft": true} // powah not included

	overrides := map[string]string{
		"minecraft:iron_plate": RecipeOptionKey(overridden.ID, overridden.MachineMod, overridden.MachineID),
	}
	g, err := s.BuildRecipeGraph(context.Background(), resource.Ref{ModID: "minecraft", ID: "iron_plate"},
		map[string]bool{}, FactoryState{}, overrides, map[string]string{})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	node := g.Nodes["minecraft:iron_plate"]
	if node == nil {
		t.Fatal("expected a node for the root item")
	}
	if !node.IsRawMaterial {
		t.Errorf("expected IsRawMaterial=true when the overridden recipe is filtered out, even though another active recipe (%s) exists — got node=%+v", active.ID, node)
	}
	if node.RecipeID != "" {
		t.Errorf("expected no recipe silently substituted for the stale override, got %q", node.RecipeID)
	}
}
