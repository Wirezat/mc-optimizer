package solver

import (
	"context"
	"testing"
)

// tagStub is a minimal RecipeStore for tag-resolution tests: one recipe whose input is a
// tag with two members.
type tagStub struct {
	recipe  *RecipeRow
	byItem  map[string][]*RecipeRow
	members []ItemRef
}

func (s *tagStub) GetRecipesForItem(_ context.Context, modID, itemID string) ([]*RecipeRow, error) {
	return s.byItem[modID+":"+itemID], nil
}
func (s *tagStub) GetRecipesForFluid(_ context.Context, _, _ string) ([]*RecipeRow, error) {
	return nil, nil
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
func (s *tagStub) GetTagMembers(_ context.Context, tagName string) ([]ItemRef, error) {
	if tagName == "c:raw_materials/copper" {
		return s.members, nil
	}
	return nil, nil
}

// newTagStub builds a store where a Macerator recipe turns #c:raw_materials/copper into
// copper_dust, and the tag has two members.
func newTagStub() *tagStub {
	tagName := "c:raw_materials/copper"
	mc, modb := "minecraft", "modb"
	dust := "copper_dust"
	recipe := &RecipeRow{
		ID:            "recipe:copper_dust",
		MachineMod:    "modern_industrialization",
		MachineID:     "macerator",
		DurationTicks: 100,
		ItemInputs: []RecipeRowItemIO{
			{TagName: &tagName, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: &dust, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
	return &tagStub{
		recipe: recipe,
		byItem: map[string][]*RecipeRow{"minecraft:copper_dust": {recipe}},
		members: []ItemRef{
			{ModID: mc, ItemID: "raw_copper"},
			{ModID: modb, ItemID: "raw_copper"},
		},
	}
}

// A ChainItem produced by resolving a multi-member tag must carry the tag's key so the
// frontend can render it as a cycling tag icon (JEI-style) instead of a plain, static item
// icon — the underlying item is still just one of several the recipe would accept.
func TestDiscover_TagResolvedItemCarriesOriginTag(t *testing.T) {
	stub := newTagStub()
	s := NewSolver(stub, 1000)
	ctx := context.Background()

	overrideKey := RecipeOptionKey("recipe:copper_dust", "modern_industrialization", "macerator")
	res, err := s.Discover(ctx, ItemRef{ModID: "minecraft", ItemID: "copper_dust"},
		map[string]bool{}, FactoryState{},
		map[string]string{"minecraft:copper_dust": overrideKey},
		map[string]string{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	var rawCopper *ChainItem
	for i := range res.Items {
		if res.Items[i].Item.ModID == "minecraft" && res.Items[i].Item.ItemID == "raw_copper" {
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
