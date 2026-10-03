package solver

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

func withKinds(t *testing.T, item, fluid resource.ExpandRule) {
	t.Helper()
	if err := resource.Load([]resource.KindInfo{
		{Kind: resource.KindItem, KeyPrefix: "", BaseUnit: "one", Expand: item},
		{Kind: resource.KindFluid, KeyPrefix: "fluid:", BaseUnit: "mb", UOMSystem: "volume", Expand: fluid},
		{Kind: resource.KindEnergy, KeyPrefix: "energy:", BaseUnit: "fe", UOMSystem: "energy", Expand: resource.ExpandNever},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(resource.LoadForTest)
}

func steamChainStore() *stubStore {
	steam := &RecipeRow{
		ID: "boil", MachineMod: "mi", MachineID: "boiler", DurationTicks: 20,
		Inputs:  []resource.IO{itemIO("minecraft", "coal", 1, 1)},
		Outputs: []resource.IO{fluidIO("mi", "steam", 100)},
	}
	turbine := &RecipeRow{
		ID: "spin", MachineMod: "mi", MachineID: "turbine", DurationTicks: 20,
		Inputs:  []resource.IO{fluidIO("mi", "steam", 100)},
		Outputs: []resource.IO{itemIO("mi", "power", 1, 1)},
	}
	return &stubStore{
		recipes: map[string]*RecipeRow{steam.ID: steam, turbine.ID: turbine},
		byItem:  map[string][]*RecipeRow{"mi:power": {turbine}},
		byFluid: map[string][]*RecipeRow{"mi:steam": {steam}},
	}
}

func buildPowerGraph(t *testing.T, overrides map[string]string) *RecipeGraph {
	t.Helper()
	g, err := NewSolver(steamChainStore(), 0).BuildRecipeGraph(context.Background(),
		resource.Ref{ModID: "mi", ID: "power"}, nil, FactoryState{}, overrides, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	return g
}

func TestExpand_FluidOnChoiceNeedsAnOverride(t *testing.T) {
	withKinds(t, resource.ExpandAlways, resource.ExpandOnChoice)
	if n := buildPowerGraph(t, nil).Nodes["fluid:mi:steam"]; n == nil || !n.IsRawMaterial {
		t.Errorf("steam without override = %+v, want a raw material", n)
	}
	if n := buildPowerGraph(t, map[string]string{"fluid:mi:steam": "boil"}).Nodes["fluid:mi:steam"]; n == nil || n.RecipeID != "boil" {
		t.Errorf("steam with override = %+v, want recipe boil", n)
	}
}

func TestExpand_FluidAlwaysFollowsRecipesFromTheTable(t *testing.T) {
	withKinds(t, resource.ExpandAlways, resource.ExpandAlways)
	g := buildPowerGraph(t, nil)
	if n := g.Nodes["fluid:mi:steam"]; n == nil || n.RecipeID != "boil" {
		t.Errorf("steam = %+v, want recipe boil without an override", n)
	}
	if n := g.Nodes["minecraft:coal"]; n == nil {
		t.Error("coal is missing, the chain stopped at steam")
	}
}

func TestExpand_ItemNeverIsARawMaterial(t *testing.T) {
	withKinds(t, resource.ExpandNever, resource.ExpandOnChoice)
	if n := buildPowerGraph(t, nil).Nodes["mi:power"]; n == nil || !n.IsRawMaterial {
		t.Errorf("power = %+v, want a raw material when items never expand", n)
	}
}

func TestRefFromKeyInvertsKey(t *testing.T) {
	for _, r := range []resource.Ref{
		{ModID: "minecraft", ID: "stone"},
		{ModID: "mi", ID: "steam", Kind: resource.KindFluid},
		{TagRef: "c:ingots"},
		{TagRef: "c:honey", Kind: resource.KindFluid},
	} {
		got := resource.RefFromKey(r.Key())
		if got.Key() != r.Key() || got.Kind.Or() != r.Kind.Or() {
			t.Errorf("resource.RefFromKey(%q) = %+v", r.Key(), got)
		}
	}
}

func toolChainStore() *stubStore {
	press := &RecipeRow{
		ID: "press", MachineMod: "mi", MachineID: "packer", DurationTicks: 20,
		Inputs: []resource.IO{
			itemIO("mi", "ingot", 9, 1),
			{Ref: resource.Ref{ModID: "mi", ID: "template"}, Amount: resource.NewRational(1, 1), Prob: resource.NewRational(1, 1), Consumed: false},
		},
		Outputs: []resource.IO{itemIO("mi", "block", 1, 1)},
	}
	return &stubStore{
		recipes: map[string]*RecipeRow{press.ID: press},
		byItem:  map[string][]*RecipeRow{"mi:block": {press}},
	}
}

func TestToolIsNotConsumed(t *testing.T) {
	g, err := NewSolver(toolChainStore(), 0).BuildRecipeGraph(context.Background(),
		resource.Ref{ModID: "mi", ID: "block"}, nil, FactoryState{}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	got, err := SolveDAG(g, resource.NewRational(1, 20))
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}
	inputs := map[string]resource.Rational{}
	for _, e := range ComputeIOProfile(got, g, FactoryState{}, "t").Inputs {
		inputs[e.Item.Key()] = e.Rate
	}
	if r, ok := inputs["mi:template"]; ok && !r.IsZero() {
		t.Errorf("template input = %s, want none for a tool", r)
	}
	if r := inputs["mi:ingot"]; !r.Eq(resource.NewRational(9, 20)) {
		t.Errorf("ingot input = %s, want 9/20", r)
	}
}

func TestToolHasNoStoichiometryCoefficient(t *testing.T) {
	store := toolChainStore()
	cut := &RecipeRow{
		ID: "cut", MachineMod: "mi", MachineID: "cutter", DurationTicks: 20,
		Inputs:  []resource.IO{itemIO("mi", "plate", 1, 1)},
		Outputs: []resource.IO{itemIO("mi", "template", 1, 1)},
	}
	store.recipes[cut.ID] = cut
	store.byItem["mi:template"] = []*RecipeRow{cut}
	g, err := NewSolver(store, 0).BuildRecipeGraph(context.Background(),
		resource.Ref{ModID: "mi", ID: "block"}, nil, FactoryState{}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	S, items, recipes := BuildStoichiometryMatrix(g)
	row, col := -1, -1
	for i, it := range items {
		if it.Key() == "mi:template" {
			row = i
		}
	}
	for j, r := range recipes {
		if r == g.Nodes["mi:block"].RateKey() {
			col = j
		}
	}
	if row < 0 || col < 0 {
		t.Fatalf("template row %d, press column %d not in the matrix", row, col)
	}
	if !S[row][col].IsZero() {
		t.Errorf("S[template][press] = %s, want 0 for a tool", S[row][col])
	}
}
