package solver

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

func honeyStore(members []resource.Ref) *stubStore {
	r := &RecipeRow{
		ID: "canning", MachineMod: "mi", MachineID: "canning_machine", DurationTicks: 100,
		Inputs: []resource.IO{
			itemIO("minecraft", "glass_bottle", 1, 1),
			tagIO(resource.KindFluid, "c:honey", 250),
		},
		Outputs: []resource.IO{itemIO("minecraft", "honey_bottle", 1, 1)},
	}
	return &stubStore{
		recipes: map[string]*RecipeRow{r.ID: r},
		byItem:  map[string][]*RecipeRow{"minecraft:honey_bottle": {r}},
		tags:    map[string][]resource.Ref{"fluid:#c:honey": members},
	}
}

func TestItemRefKey_FluidTag(t *testing.T) {
	fluidTag := resource.Ref{TagRef: "c:honey", Kind: resource.KindFluid}
	itemTag := resource.Ref{TagRef: "c:ingots"}
	if got := fluidTag.Key(); got != "fluid:#c:honey" {
		t.Errorf("fluid tag key = %q, want fluid:#c:honey", got)
	}
	if got := itemTag.Key(); got != "#c:ingots" {
		t.Errorf("item tag key = %q, want #c:ingots", got)
	}
}

func TestBuildRecipeGraph_ResolvesFluidTagToAFluid(t *testing.T) {
	store := honeyStore([]resource.Ref{{ModID: "extended_industrialization", ID: "honey", Kind: resource.KindFluid}})
	g, err := NewSolver(store, 0).BuildRecipeGraph(context.Background(),
		resource.Ref{ModID: "minecraft", ID: "honey_bottle"}, nil, FactoryState{}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	res, ok := g.TagResolutions["fluid:#c:honey"]
	if !ok || res.Chosen.Key() != "fluid:extended_industrialization:honey" {
		t.Fatalf("resolution = %+v, %v; want extended_industrialization:honey as a fluid", res, ok)
	}
	var fluidIn *Edge
	for i, e := range g.Nodes["minecraft:honey_bottle"].Inputs {
		if e.Item.Kind == resource.KindFluid {
			fluidIn = &g.Nodes["minecraft:honey_bottle"].Inputs[i]
		}
	}
	if fluidIn == nil || fluidIn.Item.Key() != "fluid:extended_industrialization:honey" {
		t.Fatalf("fluid input = %+v, want an edge to fluid:extended_industrialization:honey", fluidIn)
	}
	if n := g.Nodes["fluid:extended_industrialization:honey"]; n == nil || !n.IsRawMaterial {
		t.Errorf("honey node = %+v, want a raw material", n)
	}
	if _, bad := g.Nodes["fluid::"]; bad {
		t.Error("graph has a nameless fluid:: node")
	}
}

func TestBuildRecipeGraph_FluidTagWithoutMembersIsRawMaterial(t *testing.T) {
	g, err := NewSolver(honeyStore(nil), 0).BuildRecipeGraph(context.Background(),
		resource.Ref{ModID: "minecraft", ID: "honey_bottle"}, nil, FactoryState{}, nil, nil)
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	if n := g.Nodes["fluid:#c:honey"]; n == nil || !n.IsRawMaterial {
		t.Errorf("fluid:#c:honey node = %+v, want a raw material", n)
	}
	if _, bad := g.Nodes["fluid::"]; bad {
		t.Error("graph has a nameless fluid:: node")
	}
}

func TestBuildRecipeGraph_FluidTagOverridePicksMember(t *testing.T) {
	store := honeyStore([]resource.Ref{
		{ModID: "create", ID: "honey", Kind: resource.KindFluid},
		{ModID: "extended_industrialization", ID: "honey", Kind: resource.KindFluid},
	})
	g, err := NewSolver(store, 0).BuildRecipeGraph(context.Background(),
		resource.Ref{ModID: "minecraft", ID: "honey_bottle"}, nil, FactoryState{}, nil,
		map[string]string{"fluid:#c:honey": "extended_industrialization:honey"})
	if err != nil {
		t.Fatalf("BuildRecipeGraph: %v", err)
	}
	chosen := g.TagResolutions["fluid:#c:honey"].Chosen
	if got := chosen.Key(); got != "fluid:extended_industrialization:honey" {
		t.Errorf("chosen = %q, want the override", got)
	}
}

func TestDiscover_FluidTagInputUsesItsKey(t *testing.T) {
	store := honeyStore([]resource.Ref{{ModID: "extended_industrialization", ID: "honey", Kind: resource.KindFluid}})
	res, err := NewSolver(store, 0).Discover(context.Background(),
		resource.Ref{ModID: "minecraft", ID: "honey_bottle"}, nil, FactoryState{},
		map[string]string{"minecraft:honey_bottle": "canning"}, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var inputs []string
	var resolved *ChainItem
	for i, ci := range res.Items {
		if ci.Item.Key() == "minecraft:honey_bottle" && len(ci.Options) > 0 {
			inputs = ci.Options[0].Inputs
		}
		if ci.Item.Key() == "fluid:extended_industrialization:honey" {
			resolved = &res.Items[i]
		}
	}
	found := false
	for _, in := range inputs {
		found = found || in == "fluid:#c:honey"
	}
	if !found {
		t.Errorf("option inputs = %v, want fluid:#c:honey among them", inputs)
	}
	if resolved == nil || resolved.ResolvedTag != "fluid:#c:honey" {
		t.Errorf("chain item for honey = %+v, want ResolvedTag fluid:#c:honey", resolved)
	}
}
