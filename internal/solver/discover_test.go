package solver

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// tagStub is a RecipeStore with one recipe whose input is a two-member tag.
type tagStub struct {
	recipe  *RecipeRow
	byItem  map[string][]*RecipeRow
	members []resource.Ref
}

func (s *tagStub) GetRecipesFor(_ context.Context, ref resource.Ref) ([]*RecipeRow, error) {
	if ref.Kind.Or() != resource.KindItem {
		return nil, nil
	}
	return s.byItem[ref.ModID+":"+ref.ID], nil
}
func (s *tagStub) GetRecipe(_ context.Context, id string) (*RecipeRow, error) {
	if id == s.recipe.ID {
		return s.recipe, nil
	}
	return nil, nil
}
func (s *tagStub) GetMachinesForRecipe(_ context.Context, _ string) ([]MachineRef, error) {
	return []MachineRef{{ModID: s.recipe.MachineMod, MachineID: s.recipe.MachineID}}, nil
}
func (s *tagStub) GetMachineType(_ context.Context, modID, machineID string) (*MachineSpec, error) {
	return &MachineSpec{ModID: modID, MachineID: machineID, Name: machineID}, nil
}
func (s *tagStub) GetTagMembers(_ context.Context, tag resource.Ref) ([]resource.Ref, error) {
	if tag.TagRef == "c:raw_materials/copper" && tag.Kind.Or() == resource.KindItem {
		return s.members, nil
	}
	return nil, nil
}

// newTagStub builds a store where a Macerator recipe turns #c:raw_materials/copper into
// copper_dust, and the tag has two members.
func newTagStub() *tagStub {
	mc, modb := "minecraft", "modb"
	recipe := &RecipeRow{
		ID:            "recipe:copper_dust",
		MachineMod:    "modern_industrialization",
		MachineID:     "macerator",
		DurationTicks: 100,
		Inputs:        []resource.IO{tagIO(resource.KindItem, "c:raw_materials/copper", 1)},
		Outputs:       []resource.IO{itemIO(mc, "copper_dust", 1, 1)},
	}
	return &tagStub{
		recipe: recipe,
		byItem: map[string][]*RecipeRow{"minecraft:copper_dust": {recipe}},
		members: []resource.Ref{
			{ModID: mc, ID: "raw_copper"},
			{ModID: modb, ID: "raw_copper"},
		},
	}
}

func TestDiscover_TagResolvedItemCarriesOriginTag(t *testing.T) {
	stub := newTagStub()
	s := NewSolver(stub, 1000)
	ctx := context.Background()

	overrideKey := RecipeOptionKey("recipe:copper_dust", "modern_industrialization", "macerator")
	res, err := s.Discover(ctx, resource.Ref{ModID: "minecraft", ID: "copper_dust"},
		map[string]bool{}, FactoryState{},
		map[string]string{"minecraft:copper_dust": overrideKey},
		map[string]string{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	var rawCopper *ChainItem
	for i := range res.Items {
		if res.Items[i].Item.ModID == "minecraft" && res.Items[i].Item.ID == "raw_copper" {
			rawCopper = &res.Items[i]
		}
	}
	if rawCopper == nil {
		t.Fatalf("expected a chain item for minecraft:raw_copper, got %+v", res.Items)
	}

	const wantTagKey = "#c:raw_materials/copper"
	if rawCopper.ResolvedTag != wantTagKey {
		t.Errorf("ResolvedTag = %q, want %q", rawCopper.ResolvedTag, wantTagKey)
	}

	resolution, ok := res.TagResolutions[wantTagKey]
	if !ok {
		t.Fatalf("expected TagResolutions[%q] to be set", wantTagKey)
	}
	if len(resolution.Options) != 2 {
		t.Errorf("TagResolutions[%q].Options = %d members, want 2", wantTagKey, len(resolution.Options))
	}
}
