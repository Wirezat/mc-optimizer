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

// TestApplyUpgrade_capsSlots verifies applyUpgrade's own clamp is the package-wide
// maxUpgradeSlots safety net, NOT machine.MaxSlots (a field that has no bearing on
// upgrade capacity — see upgradeSlotCap's doc comment). Real per-tier capping (e.g.
// quantum_upgrade's stacksTo(1)) is the caller's job, done via upgradeSlotCap(tier)
// before calling applyUpgrade — tested separately in TestUpgradeSlotCap_perTier.
func TestApplyUpgrade_capsSlots(t *testing.T) {
	r := electricRecipe()
	m := electricStub().machines["modern_industrialization:macerator"]
	g := applyUpgrade(MachineGroupDraft{}, r, m, 16, 99, NewRational(1, 1))
	if g.UpgradeCount != maxUpgradeSlots {
		t.Errorf("UpgradeCount = %d, want %d (capped at maxUpgradeSlots)", g.UpgradeCount, maxUpgradeSlots)
	}
}

// TestUpgradeSlotCap_perTier verifies the cap comes from the upgrade TIER's own max
// stack size (MI: UpgradeComponent holds one ItemStack; quantum_upgrade is
// stacksTo(1)), not from the machine — the exact bug the user caught: machine_types
// had arbitrary per-machine "max_slots" numbers (2-9) that don't correspond to any
// real MI mechanic, when the real cap is uniform per upgrade item (64 standard
// stack, 1 for quantum).
func TestUpgradeSlotCap_perTier(t *testing.T) {
	standardStack := &UpgradeTierSpec{ID: "tier:basic", EUBonusPerSlot: 2, MaxStackSize: 64}
	quantum := &UpgradeTierSpec{ID: "tier:quantum", EUBonusPerSlot: 999999999, MaxStackSize: 1}
	noStackSizeRecorded := &UpgradeTierSpec{ID: "tier:legacy", EUBonusPerSlot: 2}

	if got := upgradeSlotCap(standardStack); got != 64 {
		t.Errorf("standard stack cap = %d, want 64", got)
	}
	if got := upgradeSlotCap(quantum); got != 1 {
		t.Errorf("quantum stack cap = %d, want 1 (stacksTo(1) in MI source)", got)
	}
	if got := upgradeSlotCap(noStackSizeRecorded); got != maxUpgradeSlots {
		t.Errorf("unset MaxStackSize should fall back to maxUpgradeSlots, got %d", got)
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

// highEURecipe needs 2000 EU/t — more than macerator's base (32 EU/t) or even
// the strongest tier electricStub defines (advanced_upgrade, bonus=16) at its
// full real-MI stack size (64) can ever supply: 32 + 64*16 = 1056, still below
// 2000. This recipe is genuinely unrunnable on this machine at any upgrade
// level electricStub knows about, mirroring MI's banRecipe for an
// under-tiered machine (no quantum_upgrade available in this stub's tier set).
func highEURecipe() *RecipeRow {
	return &RecipeRow{
		ID:            "recipe:high_eu",
		MachineMod:    "modern_industrialization",
		MachineID:     "macerator",
		DurationTicks: 100,
		EUPerTick:     2000,
		TotalEU:       200000,
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: sp("modern_industrialization"), ItemID: sp("hard_plate"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
}

// midEURecipe needs 64 EU/t — unreachable at base (32) or with only
// basic_upgrade (32+2=34), but reachable with advanced_upgrade (32+16=48 for
// 1 slot, 32+2*16=64 for 2 slots).
func midEURecipe() *RecipeRow {
	return &RecipeRow{
		ID:            "recipe:mid_eu",
		MachineMod:    "modern_industrialization",
		MachineID:     "macerator",
		DurationTicks: 100,
		EUPerTick:     64,
		TotalEU:       6400,
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: sp("modern_industrialization"), ItemID: sp("mid_plate"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
}

func TestRecipeBanned_basic(t *testing.T) {
	m := electricStub().machines["modern_industrialization:macerator"]
	mid := midEURecipe()

	if !recipeBanned(mid, m, 0, 0) {
		t.Error("64 EU/t recipe on a 32 EU/t base machine with no upgrades should be banned")
	}
	if !recipeBanned(mid, m, 2, 4) { // basic_upgrade x4 = 32+8=40, still < 64
		t.Error("64 EU/t recipe should still be banned with only basic_upgrade x4 (cap 40)")
	}
	if recipeBanned(mid, m, 16, 2) { // advanced_upgrade x2 = 32+32=64, exactly enough
		t.Error("64 EU/t recipe should NOT be banned with advanced_upgrade x2 (cap 64)")
	}

	high := highEURecipe()
	if !recipeBanned(high, m, 16, 4) { // far below 2000: 32+4*16=96
		t.Error("2000 EU/t recipe should be banned at this bonus/slot count (cap 96 < 2000)")
	}

	// Non-EU machines are never banned by this check (matches banRecipe's default only
	// applying to EU-tracked crafters; steam/fuel machines use a different mechanic).
	steamMachine := &MachineSpec{ModID: "modern_industrialization", MachineID: "coke_oven", EnergyType: "eu"}
	if recipeBanned(nil, steamMachine, 0, 0) {
		t.Error("nil recipe should never be reported as banned")
	}
	furnace := newStub().machines["minecraft:furnace"]
	if recipeBanned(mid, furnace, 0, 0) {
		t.Error("non-eu machine should never ban a recipe via this check")
	}
}

// TestOptimizeUpgrades_offModeWarnsWhenUnrunnable: UpgradeMode=off (or unset) must still
// check whether the recipe can run at the machine's bare base cap — a recipe needing more
// EU/t than the machine can ever supply is not "slow", it's impossible, and must warn.
func TestOptimizeUpgrades_offModeWarnsWhenUnrunnable(t *testing.T) {
	store := electricStub()
	store.recipes["recipe:mid_eu"] = midEURecipe()
	s := NewSolver(store, 1000)
	m := store.machines["modern_industrialization:macerator"]
	base := applyUpgrade(MachineGroupDraft{MachineMod: "modern_industrialization", MachineID: "macerator", RecipeID: "recipe:mid_eu"},
		midEURecipe(), m, 0, 0, NewRational(1, 1))

	out, warns, err := s.OptimizeUpgrades(context.Background(), []MachineGroupDraft{base}, SolveRequest{UpgradeMode: UpgradeModeOff})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	if len(warns) != 1 || warns[0].Code != "recipe_energy_insufficient" {
		t.Fatalf("expected one recipe_energy_insufficient warning, got %+v", warns)
	}
	if out[0].UpgradeCount != 0 {
		t.Errorf("off mode must not apply any upgrade, got n=%d", out[0].UpgradeCount)
	}
}

// TestOptimizeUpgrades_fixedWarnsWhenStillInsufficient: a Fixed-mode choice that still
// can't meet the recipe's EU/t demand (wrong tier or too few slots) must warn, not silently
// report a bogus (too-slow-but-plausible) machine count.
func TestOptimizeUpgrades_fixedWarnsWhenStillInsufficient(t *testing.T) {
	store := electricStub()
	store.recipes["recipe:mid_eu"] = midEURecipe()
	s := NewSolver(store, 1000)
	m := store.machines["modern_industrialization:macerator"]
	base := applyUpgrade(MachineGroupDraft{MachineMod: "modern_industrialization", MachineID: "macerator", RecipeID: "recipe:mid_eu"},
		midEURecipe(), m, 0, 0, NewRational(1, 1))

	// basic_upgrade x4 = 32+8=40 EU/t, recipe needs 64 — still insufficient.
	out, warns, err := s.OptimizeUpgrades(context.Background(), []MachineGroupDraft{base}, SolveRequest{
		UpgradeMode: UpgradeModeFixed, UpgradeTier: "tier:basic", UpgradeCount: 4,
	})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	if len(warns) != 1 || warns[0].Code != "recipe_energy_insufficient" {
		t.Fatalf("expected recipe_energy_insufficient warning, got %+v", warns)
	}
	if out[0].UpgradeCount != 4 || out[0].UpgradeTier != "tier:basic" {
		t.Errorf("fixed mode must still apply the user's explicit choice even if insufficient, got n=%d tier=%q",
			out[0].UpgradeCount, out[0].UpgradeTier)
	}

	// advanced_upgrade x2 = 32+32=64 — exactly enough, no warning expected.
	out2, warns2, err := s.OptimizeUpgrades(context.Background(), []MachineGroupDraft{base}, SolveRequest{
		UpgradeMode: UpgradeModeFixed, UpgradeTier: "tier:advanced", UpgradeCount: 2,
	})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	if len(warns2) != 0 {
		t.Errorf("expected no warnings once tier/count actually suffice, got %+v", warns2)
	}
	if out2[0].Count == 0 {
		t.Error("expected a valid machine count once the recipe is runnable")
	}
}

// TestAutoUpgrade_avoidsBannedCandidates: auto mode must never settle on a configuration
// that still bans the recipe if a valid one exists, even if an invalid config would have
// scored a numerically lower (but physically meaningless) machine count.
func TestAutoUpgrade_avoidsBannedCandidates(t *testing.T) {
	store := electricStub()
	store.recipes["recipe:mid_eu"] = midEURecipe()
	s := NewSolver(store, 1000)
	m := store.machines["modern_industrialization:macerator"]
	base := applyUpgrade(MachineGroupDraft{MachineMod: "modern_industrialization", MachineID: "macerator", RecipeID: "recipe:mid_eu"},
		midEURecipe(), m, 0, 0, NewRational(1, 1))

	out, warns, err := s.OptimizeUpgrades(context.Background(), []MachineGroupDraft{base}, SolveRequest{UpgradeMode: UpgradeModeAuto})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("auto mode should find the valid advanced_upgrade x2 config, got warnings %+v", warns)
	}
	if out[0].UpgradeTier != "tier:advanced" || out[0].UpgradeCount < 2 {
		t.Errorf("expected auto to land on advanced_upgrade with >=2 slots, got tier=%q n=%d",
			out[0].UpgradeTier, out[0].UpgradeCount)
	}
	if recipeBanned(midEURecipe(), m, 16, out[0].UpgradeCount) {
		t.Error("auto's final choice must not still ban the recipe")
	}
}

// TestAutoUpgrade_warnsWhenNoTierEverSuffices: if NO combination of tier/slots on this
// machine can ever satisfy the recipe, auto mode must fall back gracefully (not crash,
// not silently accept a bogus count) and must warn.
func TestAutoUpgrade_warnsWhenNoTierEverSuffices(t *testing.T) {
	store := electricStub()
	store.recipes["recipe:high_eu"] = highEURecipe()
	s := NewSolver(store, 1000)
	m := store.machines["modern_industrialization:macerator"]
	base := applyUpgrade(MachineGroupDraft{MachineMod: "modern_industrialization", MachineID: "macerator", RecipeID: "recipe:high_eu"},
		highEURecipe(), m, 0, 0, NewRational(1, 1))

	out, warns, err := s.OptimizeUpgrades(context.Background(), []MachineGroupDraft{base}, SolveRequest{UpgradeMode: UpgradeModeAuto})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	if len(warns) != 1 || warns[0].Code != "recipe_energy_insufficient" {
		t.Fatalf("expected recipe_energy_insufficient warning when structurally unrunnable, got %+v", warns)
	}
	if out[0].Count <= 0 {
		t.Error("group must still report SOME machine count, not crash or zero out")
	}
}

// TestRecipeBanned_fixedCapIgnoresUpgrades models MI's EBF coil-tier system: a machine
// with FixedRecipeEUCap set bans any recipe above that cap NO MATTER how many upgrades
// are applied — the fixed cap is a separate, upgrade-immune ceiling from MaxEUPerTick.
func TestRecipeBanned_fixedCapIgnoresUpgrades(t *testing.T) {
	cupronickel := &MachineSpec{
		ModID: "modern_industrialization", MachineID: "electric_blast_furnace_cupronickel",
		EnergyType: "eu", Upgradable: true, MaxSlots: 4, MaxEUPerTick: 128,
		FixedRecipeEUCap: 32,
	}
	kanthal := &MachineSpec{ // same base stats, no fixed cap — represents the "electric_blast_furnace" row
		ModID: "modern_industrialization", MachineID: "electric_blast_furnace",
		EnergyType: "eu", Upgradable: true, MaxSlots: 4, MaxEUPerTick: 128,
	}

	lowEU := &RecipeRow{ID: "recipe:low", EUPerTick: 32}
	highEU := &RecipeRow{ID: "recipe:high", EUPerTick: 128}

	if recipeBanned(lowEU, cupronickel, 0, 0) {
		t.Error("32 EU/t recipe should be allowed on cupronickel (cap exactly 32)")
	}
	if !recipeBanned(highEU, cupronickel, 999999999, 4) {
		t.Error("128 EU/t recipe must stay banned on cupronickel even with a quantum-tier upgrade — the coil cap is upgrade-immune")
	}
	if recipeBanned(highEU, kanthal, 0, 0) {
		t.Error("128 EU/t recipe should be allowed on kanthal (no fixed cap, base 128 already covers it)")
	}
}

func TestOptimizeUpgrades_fixedCapWarnsRegardlessOfUpgradeCount(t *testing.T) {
	store := electricStub()
	cupronickel := &MachineSpec{
		ModID: "modern_industrialization", MachineID: "electric_blast_furnace_cupronickel",
		EnergyType: "eu", Upgradable: true, MaxSlots: 4, MaxEUPerTick: 128,
		FixedRecipeEUCap: 32,
	}
	store.machines["modern_industrialization:electric_blast_furnace_cupronickel"] = cupronickel
	highEU := &RecipeRow{ID: "recipe:ebf_high", MachineMod: "modern_industrialization", MachineID: "electric_blast_furnace_cupronickel", EUPerTick: 128, DurationTicks: 100, TotalEU: 12800}
	store.recipes["recipe:ebf_high"] = highEU
	s := NewSolver(store, 1000)

	base := applyUpgrade(MachineGroupDraft{MachineMod: "modern_industrialization", MachineID: "electric_blast_furnace_cupronickel", RecipeID: "recipe:ebf_high"},
		highEU, cupronickel, 0, 0, NewRational(1, 1))

	// Even auto mode, free to pick ANY tier/slot count, must never clear this ban.
	out, warns, err := s.OptimizeUpgrades(context.Background(), []MachineGroupDraft{base}, SolveRequest{UpgradeMode: UpgradeModeAuto})
	if err != nil {
		t.Fatalf("OptimizeUpgrades: %v", err)
	}
	if len(warns) != 1 || warns[0].Code != "recipe_energy_insufficient" {
		t.Fatalf("expected recipe_energy_insufficient warning (fixed coil cap unresolvable by upgrades), got %+v", warns)
	}
	if out[0].Count <= 0 {
		t.Error("group must still report a count, not crash or zero out")
	}
}
