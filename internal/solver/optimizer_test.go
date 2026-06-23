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
	tiers    map[string][]*UpgradeTierSpec
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

func (s *stubStore) GetUpgradeTiers(_ context.Context, modID string) ([]*UpgradeTierSpec, error) {
	return s.tiers[modID], nil
}

func (s *stubStore) GetTagMembers(_ context.Context, _ string) ([]ItemRef, error) {
	return nil, nil
}

// iron ingot recipe: 1 iron ore → 1 iron ingot, 20 ticks, in furnace
func ironIngotRecipe() *RecipeRow {
	iron := "iron_ore"
	mc := "minecraft"
	return &RecipeRow{
		ID:            "recipe:iron_ingot",
		MachineMod:    "minecraft",
		MachineID:     "furnace",
		DurationTicks: 20,
		ItemInputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: &iron, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: sp("iron_ingot"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
}

func newStub() *stubStore {
	recipe := ironIngotRecipe()
	return &stubStore{
		recipes:  map[string]*RecipeRow{"recipe:iron_ingot": recipe},
		byItem:   map[string][]*RecipeRow{"minecraft:iron_ingot": {recipe}},
		machines: map[string]*MachineSpec{
			"minecraft:furnace": {ModID: "minecraft", MachineID: "furnace", EnergyType: "NONE"},
		},
	}
}

func TestSolve_singleRecipe(t *testing.T) {
	s := NewSolver(newStub(), 1000)
	req := SolveRequest{
		TargetItem: ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		TargetRate: NewRational(1, 1),
		TimeUnit:   "s",
		Mode:       SolveModeTarget,
		StopPoints: map[string]bool{},
	}
	result, err := s.Solve(context.Background(), req)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if len(result.MachineGroups) == 0 {
		t.Fatal("expected at least one machine group")
	}
}

// electricRecipe: a recipe with totalEU=400 (eu=2 × duration=200) on an MI electric machine.
func electricRecipe() *RecipeRow {
	return &RecipeRow{
		ID:            "recipe:plate",
		MachineMod:    "modern_industrialization",
		MachineID:     "macerator",
		DurationTicks: 200,
		EUPerTick:     2,
		TotalEU:       400,
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: sp("modern_industrialization"), ItemID: sp("plate"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
}

func electricStub() *stubStore {
	r := electricRecipe()
	return &stubStore{
		recipes: map[string]*RecipeRow{r.ID: r},
		machines: map[string]*MachineSpec{
			"modern_industrialization:macerator": {
				ModID: "modern_industrialization", MachineID: "macerator",
				EnergyType: "eu", Upgradable: true, MaxSlots: 4,
			},
		},
		tiers: map[string][]*UpgradeTierSpec{
			"modern_industrialization": {
				{ID: "tier:basic", EUBonusPerSlot: 2},
				{ID: "tier:advanced", EUBonusPerSlot: 16},
			},
		},
	}
}

func TestEffectiveTicks_electricOverclock(t *testing.T) {
	r := electricRecipe()
	m := electricStub().machines["modern_industrialization:macerator"]

	// Base (n=0): overclocks to 32 EU/t → ceil(400/32) = 13, not the nominal 200.
	if got := effectiveTicks(r, m, 0, 0); got != 13 {
		t.Errorf("base effectiveTicks = %d, want 13", got)
	}
	// 4 slots × 16 bonus → 32+64=96 EU/t → ceil(400/96) = 5.
	if got := effectiveTicks(r, m, 16, 4); got != 5 {
		t.Errorf("upgraded effectiveTicks = %d, want 5", got)
	}
}

func TestEffectiveTicks_nonElectricUnchanged(t *testing.T) {
	r := ironIngotRecipe() // furnace, no totalEU
	m := newStub().machines["minecraft:furnace"]
	if got := effectiveTicks(r, m, 0, 0); got != 20 {
		t.Errorf("furnace effectiveTicks = %d, want 20 (duration)", got)
	}
}

func TestApplyUpgrade_capsSlots(t *testing.T) {
	r := electricRecipe()
	m := electricStub().machines["modern_industrialization:macerator"]
	g := applyUpgrade(MachineGroupDraft{}, r, m, 16, 99, NewRational(1, 1))
	if g.UpgradeCount != 4 {
		t.Errorf("UpgradeCount = %d, want 4 (capped at MaxSlots)", g.UpgradeCount)
	}
}

func TestOptimizeUpgrades_fixed(t *testing.T) {
	s := NewSolver(electricStub(), 1000)
	groups := []MachineGroupDraft{
		applyUpgrade(MachineGroupDraft{MachineMod: "modern_industrialization", MachineID: "macerator", RecipeID: "recipe:plate"},
			electricRecipe(), electricStub().machines["modern_industrialization:macerator"], 0, 0, NewRational(1, 1)),
	}
	out, _, err := s.OptimizeUpgrades(context.Background(), groups, SolveRequest{
		UpgradeMode: UpgradeModeFixed, UpgradeTier: "tier:advanced", UpgradeCount: 4,
	})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	// recipeRate 1/tick × 5 ticks = 5 machines, tier recorded.
	if out[0].Count != 5 || out[0].UpgradeTier != "tier:advanced" || out[0].UpgradeCount != 4 {
		t.Errorf("fixed result = count %d tier %q n %d, want 5/tier:advanced/4",
			out[0].Count, out[0].UpgradeTier, out[0].UpgradeCount)
	}
}

func TestOptimizeUpgrades_autoMinimisesMachines(t *testing.T) {
	s := NewSolver(electricStub(), 1000)
	base := applyUpgrade(MachineGroupDraft{MachineMod: "modern_industrialization", MachineID: "macerator", RecipeID: "recipe:plate"},
		electricRecipe(), electricStub().machines["modern_industrialization:macerator"], 0, 0, NewRational(1, 1))

	out, _, err := s.OptimizeUpgrades(context.Background(), []MachineGroupDraft{base}, SolveRequest{UpgradeMode: UpgradeModeAuto})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	// Auto should reduce below the base count of 13.
	if out[0].Count >= base.Count {
		t.Errorf("auto count %d not below base %d", out[0].Count, base.Count)
	}
	if out[0].UpgradeCount == 0 {
		t.Errorf("auto chose no upgrades, expected some")
	}
}

func TestRateVector_itemRates(t *testing.T) {
	rv := newRateVector()
	rv.RecipeRates["recipe:iron_ingot"] = NewRational(1, 20)
	rv.ItemRates["minecraft:iron_ingot"] = NewRational(1, 20)

	if rv.RecipeRates["recipe:iron_ingot"].Den != 20 {
		t.Errorf("unexpected rate: %v", rv.RecipeRates["recipe:iron_ingot"])
	}
}
