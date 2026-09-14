package solver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

func TestDefaultVariantUsesNominalDuration(t *testing.T) {
	v := DefaultVariant(&RecipeRow{ID: "r1", DurationTicks: 20})
	if v.Rate.Num != 1 || v.Rate.Den != 20 {
		t.Errorf("Rate = %d/%d, want 1/20", v.Rate.Num, v.Rate.Den)
	}
	if len(v.Costs) != 0 || len(v.Items) != 0 {
		t.Errorf("Costs=%+v Items=%+v, want both empty", v.Costs, v.Items)
	}
	if !v.Valid {
		t.Error("Valid = false, want true")
	}
}

func TestDefaultVariantClampsZeroDuration(t *testing.T) {
	if v := DefaultVariant(&RecipeRow{ID: "r1", DurationTicks: 0}); v.Rate.Den != 1 {
		t.Errorf("Den = %d, want 1 for a zero-duration recipe", v.Rate.Den)
	}
}

func TestPickVariantMinimisesMachineCount(t *testing.T) {
	vs := []plugins.Variant{
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true},
		{ID: "fast", Rate: plugins.Rational{Num: 1, Den: 10}, Valid: true,
			Items: []plugins.Item{{Ref: "m:upg", Count: 2}}},
	}
	// Demand: 1 recipe every 10 ticks. base achieves 1/20 -> 2 machines,
	// fast achieves 1/10 -> 1 machine.
	got, count, ok := PickVariant(vs, NewRational(1, 10))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.ID != "fast" {
		t.Errorf("ID = %q, want %q", got.ID, "fast")
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestPickVariantBreaksTieOnFewerItems(t *testing.T) {
	vs := []plugins.Variant{
		// "wasteful" is listed first on purpose: a tie-break that fell back to
		// loop order (first-found-wins) would return it, so only a genuine
		// fewer-items comparison can make "cheap" win here.
		{ID: "wasteful", Rate: plugins.Rational{Num: 1, Den: 10}, Valid: true,
			Items: []plugins.Item{{Ref: "m:upg", Count: 8}}},
		{ID: "cheap", Rate: plugins.Rational{Num: 1, Den: 10}, Valid: true,
			Items: []plugins.Item{{Ref: "m:upg", Count: 1}}},
	}
	got, _, ok := PickVariant(vs, NewRational(1, 10))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.ID != "cheap" {
		t.Errorf("ID = %q, want %q", got.ID, "cheap")
	}
}

func TestPickVariantSkipsInvalid(t *testing.T) {
	vs := []plugins.Variant{
		// "impossible" is far faster than "base": if the skip guard let it
		// through, it would need strictly fewer machines (1 vs 2), not just
		// tie with "base". That makes the result independent of list order,
		// unlike a tie where a disabled guard could still "accidentally"
		// produce the expected winner depending on which entry comes first.
		{ID: "impossible", Rate: plugins.Rational{Num: 1, Den: 1}, Valid: false},
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true},
	}
	// Demand: 1 recipe every 10 ticks. base needs 2 machines at 1/20;
	// impossible, if not skipped, would need only 1 at 1/1.
	got, _, ok := PickVariant(vs, NewRational(1, 10))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.ID != "base" {
		t.Errorf("ID = %q, want %q", got.ID, "base")
	}
}

func TestPickVariantReportsNoValidOption(t *testing.T) {
	vs := []plugins.Variant{{ID: "impossible", Rate: plugins.Rational{Num: 1, Den: 1}, Valid: false}}
	if _, _, ok := PickVariant(vs, NewRational(1, 20)); ok {
		t.Error("ok = true, want false when no variant is valid")
	}
}

// stubSource returns fixed variants and counts its calls.
type stubSource struct {
	vs    []plugins.Variant
	calls int
	err   error
}

func (s *stubSource) Variants(_ context.Context, _ *MachineSpec, r *RecipeRow, _ json.RawMessage) ([]plugins.Variant, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.vs == nil {
		return []plugins.Variant{DefaultVariant(r)}, nil
	}
	return s.vs, nil
}

func hasWarning(ws []Warning, code string) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestCalculateMachineGroupsUsesVariantSource(t *testing.T) {
	src := &stubSource{vs: []plugins.Variant{
		{ID: "base", Label: "Base", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true},
		{ID: "fast", Label: "Fast", Rate: plugins.Rational{Num: 1, Den: 5}, Valid: true,
			Items: []plugins.Item{{Ref: "m:upg", Count: 1}},
			Costs: []plugins.Cost{{Resource: "eu", Amount: plugins.Rational{Num: 128, Den: 1}}}},
	}}
	s, g, rv := newVariantTestSolver(t, src)

	groups, _, err := s.CalculateMachineGroups(context.Background(), g, rv, SolveRequest{})
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	if groups[0].VariantID != "fast" {
		t.Errorf("VariantID = %q, want %q", groups[0].VariantID, "fast")
	}
	if len(groups[0].Costs) != 1 || groups[0].Costs[0].Resource != "eu" {
		t.Errorf("Costs = %+v, want one eu cost", groups[0].Costs)
	}
	if src.calls != 1 {
		t.Errorf("VariantSource called %d times, want 1", src.calls)
	}
}

func TestCalculateMachineGroupsWarnsWhenNoVariantValid(t *testing.T) {
	src := &stubSource{vs: []plugins.Variant{
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: false},
	}}
	s, g, rv := newVariantTestSolver(t, src)

	groups, warnings, err := s.CalculateMachineGroups(context.Background(), g, rv, SolveRequest{})
	if err != nil {
		t.Fatalf("an unrunnable recipe must warn, not fail: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	if !hasWarning(warnings, "recipe_not_runnable") {
		t.Errorf("warnings = %+v, want recipe_not_runnable", warnings)
	}
}

// TestCalculateMachineGroupsSurvivesPluginError covers spec section 10: a broken
// plugin degrades its own mod instead of toppling the solve.
func TestCalculateMachineGroupsSurvivesPluginError(t *testing.T) {
	src := &stubSource{err: errors.New("plugin exploded")}
	s, g, rv := newVariantTestSolver(t, src)

	groups, warnings, err := s.CalculateMachineGroups(context.Background(), g, rv, SolveRequest{})
	if err != nil {
		t.Fatalf("a plugin error must not fail the solve: %v", err)
	}
	if groups[0].VariantID != "default" {
		t.Errorf("VariantID = %q, want %q", groups[0].VariantID, "default")
	}
	if !hasWarning(warnings, "plugin_failed") {
		t.Errorf("warnings = %+v, want plugin_failed", warnings)
	}
}

// The raw store/plugin error can be a pgx error carrying host, user and
// database name (or any other internal detail), and /api/demo/solve serves
// this result to an unauthenticated caller. The warning may name the failing
// mod, never repeat the error text.
func TestCalculateMachineGroupsPluginFailureWarningOmitsRawError(t *testing.T) {
	sensitive := "dial tcp 10.0.0.5:5432: connect: connection refused, user=mc_optimizer_admin"
	src := &stubSource{err: errors.New(sensitive)}
	s, g, rv := newVariantTestSolver(t, src)

	_, warnings, err := s.CalculateMachineGroups(context.Background(), g, rv, SolveRequest{})
	if err != nil {
		t.Fatalf("a plugin error must not fail the solve: %v", err)
	}
	for _, w := range warnings {
		if w.Code != "plugin_failed" {
			continue
		}
		for k, v := range w.Params {
			if strings.Contains(v, sensitive) {
				t.Fatalf("warning param %q = %q leaks the raw store error", k, v)
			}
		}
		if _, ok := w.Params["error"]; ok {
			t.Errorf("warning params = %+v, want no \"error\" key at all", w.Params)
		}
	}
}

func TestCalculateMachineGroupsFallsBackWithoutSource(t *testing.T) {
	s, g, rv := newVariantTestSolver(t, nil)
	groups, warnings, err := s.CalculateMachineGroups(context.Background(), g, rv, SolveRequest{})
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	if groups[0].VariantID != "default" {
		t.Errorf("VariantID = %q, want %q", groups[0].VariantID, "default")
	}
	// The nominal duration has to survive as a rate, not only as an id: 1/10
	// recipes per tick needs two furnaces running the 20-tick recipe.
	if groups[0].Count != 2 {
		t.Errorf("Count = %d, want 2", groups[0].Count)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %+v, want none for the plain host default", warnings)
	}
}

// TestCalculateMachineGroupsReportsRateOverflow covers the host-side overflow
// flank. Both operands sit inside the plugin contract on their own (a
// denominator of math.MaxInt32 is exactly the bound plugins.Validate allows),
// but the accumulated recipe rate carries no bound at all: from a numerator of
// about 2^34 upward the division panics inside rational.go. The solve must
// report that as an error rather than take the host down.
func TestCalculateMachineGroupsReportsRateOverflow(t *testing.T) {
	src := &stubSource{vs: []plugins.Variant{
		{ID: "slow", Rate: plugins.Rational{Num: 1, Den: math.MaxInt32}, Valid: true},
	}}
	s, g, rv := newVariantTestSolver(t, src)
	rv.RecipeRates[RecipeOptionKey("recipe:iron_ingot", "minecraft", "furnace")] = NewRational(1<<34, 1)

	_, _, err := s.CalculateMachineGroups(context.Background(), g, rv, SolveRequest{})
	if err == nil {
		t.Fatal("err = nil, want an overflow error for a rate that cannot be represented")
	}
	if !errors.Is(err, ErrRateOverflow) {
		t.Errorf("err = %v, want ErrRateOverflow", err)
	}
}

// solveWithVariants runs a full Solve over the iron-ingot fixture (a 20-tick
// recipe, iron ore as raw material) at the given target rate in ingots per tick.
func solveWithVariants(t *testing.T, src VariantSource, targetNum, targetDen int64) SolveResult {
	t.Helper()
	s := NewSolver(newStub(20), 1000)
	s.VariantSource = src
	res, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		TargetRate: NewRational(targetNum, targetDen),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	return res
}

// TestSolveAppliesVariantOutputOverrides covers the fixed-point round: a variant
// that yields two ingots per craft halves both the machine count and the ore
// demand. Without the second round the graph would still hold the catalog amount
// of one ingot per craft and report a full machine plus 1/20 ore per tick.
func TestSolveAppliesVariantOutputOverrides(t *testing.T) {
	src := &stubSource{vs: []plugins.Variant{
		{ID: "doubled", Label: "Doubled", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true,
			Outputs: []plugins.Output{{
				Ref:         "minecraft:iron_ingot",
				Amount:      plugins.Rational{Num: 2, Den: 1},
				Probability: plugins.Rational{Num: 1, Den: 1},
			}}},
	}}
	res := solveWithVariants(t, src, 1, 20)

	if len(res.MachineGroups) != 1 {
		t.Fatalf("want 1 group, got %d", len(res.MachineGroups))
	}
	wantRational(t, "ExactCount", res.MachineGroups[0].ExactCount, 1, 2)
	ore := ioHasInput(res.IOProfile, "iron_ore")
	if ore == nil {
		t.Fatal("iron_ore missing from the IO profile")
	}
	wantRational(t, "iron_ore input", ore.Rate, 1, 40)
	if hasWarning(res.Warnings, "variant_not_converged") {
		t.Errorf("warnings = %+v, want no convergence warning for a single applied override", res.Warnings)
	}
	if src.calls != 2 {
		t.Errorf("VariantSource called %d times, want 2 (one round applies the override, one confirms it settled)", src.calls)
	}
}

// TestSolveWarnsWhenVariantOutputsOscillate covers the iteration bound. "boosted"
// wins at the catalog output amount because it needs one machine instead of two;
// applying its four-per-craft output cuts the rate so far that both variants need
// one machine, at which point "base" wins on installed items and reverts the
// amount, which brings "boosted" back. The pair never settles, so the solve stops
// after MaxVariantIterations and says so. "base" is listed first on purpose: the
// round that flips back to it is decided by installed items (0 against 1), not by
// list position, so the oscillation does not ride on the order of this slice.
func TestSolveWarnsWhenVariantOutputsOscillate(t *testing.T) {
	src := &stubSource{vs: []plugins.Variant{
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true},
		{ID: "boosted", Rate: plugins.Rational{Num: 1, Den: 10}, Valid: true,
			Items: []plugins.Item{{Ref: "m:upg", Count: 1}},
			Outputs: []plugins.Output{{
				Ref:         "minecraft:iron_ingot",
				Amount:      plugins.Rational{Num: 4, Den: 1},
				Probability: plugins.Rational{Num: 1, Den: 1},
			}}},
	}}
	res := solveWithVariants(t, src, 1, 10)

	if !hasWarning(res.Warnings, "variant_not_converged") {
		t.Errorf("warnings = %+v, want variant_not_converged", res.Warnings)
	}
	if src.calls != MaxVariantIterations+1 {
		t.Errorf("VariantSource called %d times, want %d (the iteration budget plus the final round)",
			src.calls, MaxVariantIterations+1)
	}
}

// TestSyncVariantOutputsKeysByMachine covers the keying: both nodes run the same
// recipe row, reached through machine_interfaces, but on different machines, and
// only the bronze machine's variant overrides outputs. Keying by RecipeID alone
// would leak that override into the electric node — its own primary amount and,
// worse, its shared iron byproduct edge. The assertions hold in either group
// order: with RecipeID keying, one order leaves the map without overrides and
// nothing changes at all, the other bleeds into the electric node.
func TestSyncVariantOutputsKeysByMachine(t *testing.T) {
	iron := ItemRef{ModID: "mod", ItemID: "iron"}
	copper := ItemRef{ModID: "mod", ItemID: "copper"}
	one := NewRational(1, 1)
	edges := func() []Edge {
		return []Edge{
			{Item: iron, Amount: one, Probability: one},
			{Item: copper, Amount: one, Probability: one},
		}
	}
	bronze := &RecipeNode{Item: iron, RecipeID: "r:sep", MachineMod: "mod", MachineID: "bronze",
		OutputAmount: one, Outputs: edges()}
	electric := &RecipeNode{Item: copper, RecipeID: "r:sep", MachineMod: "mod", MachineID: "electric",
		OutputAmount: one, Outputs: edges()}
	g := &RecipeGraph{Nodes: map[string]*RecipeNode{"mod:iron": bronze, "mod:copper": electric}}
	base := captureOutputBaseline(g)

	groups := []MachineGroupDraft{
		{RecipeID: "r:sep", MachineMod: "mod", MachineID: "bronze", Variant: plugins.Variant{
			ID: "boosted", Outputs: []plugins.Output{
				{Ref: "mod:iron", Amount: plugins.Rational{Num: 4, Den: 1}, Probability: plugins.Rational{Num: 1, Den: 1}},
				{Ref: "mod:copper", Amount: plugins.Rational{Num: 1, Den: 1}, Probability: plugins.Rational{Num: 1, Den: 1}},
			}}},
		{RecipeID: "r:sep", MachineMod: "mod", MachineID: "electric", Variant: plugins.Variant{ID: "plain"}},
	}

	if !syncVariantOutputs(g, base, groups, true) {
		t.Fatal("syncVariantOutputs = false, want true: the bronze override differs from the catalog")
	}
	wantRational(t, "bronze primary", bronze.OutputAmount, 4, 1)
	wantRational(t, "bronze iron edge", bronze.Outputs[0].Amount, 4, 1)
	wantRational(t, "electric primary", electric.OutputAmount, 1, 1)
	wantRational(t, "electric iron edge", electric.Outputs[0].Amount, 1, 1)
	wantRational(t, "electric copper edge", electric.Outputs[1].Amount, 1, 1)
}

// newByproductStub is the iron-ingot fixture with a second output: 1 iron ore →
// 1 iron ingot + 1 slag in a 20-tick furnace.
func newByproductStub() *stubStore {
	mc := "minecraft"
	ore := "iron_ore"
	recipe := &RecipeRow{
		ID:            "recipe:iron_ingot",
		MachineMod:    "minecraft",
		MachineID:     "furnace",
		DurationTicks: 20,
		ItemInputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: &ore, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
		ItemOutputs: []RecipeRowItemIO{
			{ItemModID: &mc, ItemID: sp("iron_ingot"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
			{ItemModID: &mc, ItemID: sp("slag"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
	return &stubStore{
		recipes: map[string]*RecipeRow{"recipe:iron_ingot": recipe},
		byItem:  map[string][]*RecipeRow{"minecraft:iron_ingot": {recipe}},
		machines: map[string]*MachineSpec{
			"minecraft:furnace": {ModID: "minecraft", MachineID: "furnace", Name: "Furnace"},
		},
	}
}

// TestSolveKeepsGraphAndGroupsConsistentAtTheIterationCap pins what the result
// describes once the budget is spent: the byproduct rate is derived from the
// graph's output amount, so a last override applied after the groups were
// computed would report slag at a quarter of the rate those groups actually
// produce. At the cap the drift is only measured, never written.
func TestSolveKeepsGraphAndGroupsConsistentAtTheIterationCap(t *testing.T) {
	src := &stubSource{vs: []plugins.Variant{
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true},
		{ID: "boosted", Rate: plugins.Rational{Num: 1, Den: 10}, Valid: true,
			Items: []plugins.Item{{Ref: "m:upg", Count: 1}},
			Outputs: []plugins.Output{
				{Ref: "minecraft:iron_ingot", Amount: plugins.Rational{Num: 4, Den: 1}, Probability: plugins.Rational{Num: 1, Den: 1}},
				{Ref: "minecraft:slag", Amount: plugins.Rational{Num: 1, Den: 1}, Probability: plugins.Rational{Num: 1, Den: 1}},
			}},
	}}
	s := NewSolver(newByproductStub(), 1000)
	s.VariantSource = src
	res, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		TargetRate: NewRational(1, 10),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if !hasWarning(res.Warnings, "variant_not_converged") {
		t.Fatalf("warnings = %+v, want variant_not_converged", res.Warnings)
	}
	if len(res.MachineGroups) != 1 {
		t.Fatalf("want 1 group, got %d", len(res.MachineGroups))
	}
	wantRational(t, "ExactCount", res.MachineGroups[0].ExactCount, 1, 1)
	slag := ioHasOutput(res.IOProfile, "slag")
	if slag == nil {
		t.Fatal("slag missing from the IO profile")
	}
	wantRational(t, "slag output", slag.Rate, 1, 10)
}

// ── fixtures for the overflow reproductions and the multi-mod solve ─────────

// stubRecipe builds a fixture recipe whose inputs and outputs are one unit each;
// refs are "mod:item" strings.
func stubRecipe(id, machineMod, machineID string, durationTicks int, inputs, outputs []string) *RecipeRow {
	r := &RecipeRow{ID: id, MachineMod: machineMod, MachineID: machineID, DurationTicks: durationTicks}
	for _, ref := range inputs {
		mod, item, _ := strings.Cut(ref, ":")
		r.ItemInputs = append(r.ItemInputs, RecipeRowItemIO{
			ItemModID: &mod, ItemID: &item,
			AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1,
		})
	}
	for _, ref := range outputs {
		mod, item, _ := strings.Cut(ref, ":")
		r.ItemOutputs = append(r.ItemOutputs, RecipeRowItemIO{
			ItemModID: &mod, ItemID: &item,
			AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1,
		})
	}
	return r
}

// stubStoreFor indexes the given recipes by every item they produce.
func stubStoreFor(recipes []*RecipeRow, machines ...*MachineSpec) *stubStore {
	st := &stubStore{
		recipes:  make(map[string]*RecipeRow, len(recipes)),
		byItem:   make(map[string][]*RecipeRow, len(recipes)),
		machines: make(map[string]*MachineSpec, len(machines)),
	}
	for _, r := range recipes {
		st.recipes[r.ID] = r
		for _, out := range r.ItemOutputs {
			key := *out.ItemModID + ":" + *out.ItemID
			st.byItem[key] = append(st.byItem[key], r)
		}
	}
	for _, m := range machines {
		st.machines[m.ModID+":"+m.MachineID] = m
	}
	return st
}

// chainStub is a chain of `stages` recipes: mc:t0 (raw) → mc:t1 → … → mc:tN,
// every step one unit in, one unit out, on the same 20-tick machine.
func chainStub(stages int) (*stubStore, ItemRef) {
	recipes := make([]*RecipeRow, 0, stages)
	for i := 1; i <= stages; i++ {
		recipes = append(recipes, stubRecipe(
			fmt.Sprintf("recipe:t%d", i), "minecraft", "furnace", 20,
			[]string{fmt.Sprintf("mc:t%d", i-1)}, []string{fmt.Sprintf("mc:t%d", i)}))
	}
	store := stubStoreFor(recipes, &MachineSpec{ModID: "minecraft", MachineID: "furnace", Name: "Furnace"})
	return store, ItemRef{ModID: "mc", ItemID: fmt.Sprintf("t%d", stages)}
}

// overrideSource answers with a single variant that echoes the recipe's own
// output refs at the configured amounts: amounts[ref] first, then all, then the
// catalog amount. rates[recipeID] overrides the default rate per recipe.
type overrideSource struct {
	rate    plugins.Rational
	rates   map[string]plugins.Rational
	all     *plugins.Rational
	amounts map[string]plugins.Rational
}

func (o *overrideSource) Variants(_ context.Context, _ *MachineSpec, r *RecipeRow, _ json.RawMessage) ([]plugins.Variant, error) {
	rate := o.rate
	if got, ok := o.rates[r.ID]; ok {
		rate = got
	}
	v := plugins.Variant{ID: "override", Rate: rate, Valid: true}
	for _, out := range r.ItemOutputs {
		ref := *out.ItemModID + ":" + *out.ItemID
		amount := plugins.Rational{Num: out.AmountNum, Den: out.AmountDen}
		if o.all != nil {
			amount = *o.all
		}
		if got, ok := o.amounts[ref]; ok {
			amount = got
		}
		v.Outputs = append(v.Outputs, plugins.Output{
			Ref: ref, Amount: amount, Probability: plugins.Rational{Num: 1, Den: 1},
		})
	}
	return []plugins.Variant{v}, nil
}

// maxPluginRational is the largest magnitude plugins.Validate admits, so every
// number in the overflow reproductions below is one a conforming plugin may
// return. None of them is extreme by itself; the chains they feed are.
const maxPluginRational = int64(math.MaxInt32)

// TestSolveSurvivesOverflowInRateSolving is reproduction 1: a three-stage chain
// at a target of one item per tick, every variant overriding its primary output
// to 1/MaxInt32. The rate is squared at every stage, so SolveDAG (dag.go, the
// itemRate/OutputAmount division) overflows on the second pass through the
// fixed-point loop — far outside chooseVariant's local guard.
func TestSolveSurvivesOverflowInRateSolving(t *testing.T) {
	tiny := plugins.Rational{Num: 1, Den: maxPluginRational}
	store, target := chainStub(3)
	s := NewSolver(store, 1000)
	s.VariantSource = &overrideSource{rate: plugins.Rational{Num: 1, Den: 20}, all: &tiny}

	_, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: target,
		TargetRate: NewRational(1, 1),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
	})
	if err == nil {
		t.Fatal("err = nil, want an arithmetic error for a rate that cannot be represented")
	}
	if !errors.Is(err, ErrRateOverflow) {
		t.Errorf("err = %v, want ErrRateOverflow", err)
	}
}

// TestSolveSurvivesOverflowInIOProfile is reproduction 2: the graph and the
// machine groups solve cleanly, and the overflow only happens afterwards in
// ComputeIOProfile, where the byproduct rate is the primary rate divided by a
// 1/MaxInt32 output and then multiplied by a MaxInt32 byproduct.
func TestSolveSurvivesOverflowInIOProfile(t *testing.T) {
	s := NewSolver(newByproductStub(), 1000)
	s.VariantSource = &overrideSource{
		rate: plugins.Rational{Num: 1, Den: 20},
		amounts: map[string]plugins.Rational{
			"minecraft:iron_ingot": {Num: 1, Den: maxPluginRational},
			"minecraft:slag":       {Num: maxPluginRational, Den: 1},
		},
	}

	_, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		TargetRate: NewRational(4, 1),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
	})
	if err == nil {
		t.Fatal("err = nil, want an arithmetic error from the IO profile")
	}
	if !errors.Is(err, ErrRateOverflow) {
		t.Errorf("err = %v, want ErrRateOverflow", err)
	}
}

// TestSolveSurvivesOverflowInLinearSystem is reproduction 3: the same class on a
// cyclic graph. The fixture is the solvable reactor cycle — the reactor's surplus
// of depleted cells is what closes it — and every output is scaled down by the
// same MaxInt32, which keeps the system solvable but lifts the solution far
// enough for Gauss-Jordan to overflow while eliminating.
func TestSolveSurvivesOverflowInLinearSystem(t *testing.T) {
	reactor := stubRecipe("recipe:reactor", "mi", "reactor", 100,
		[]string{"mi:enriched_uranium", "mi:coolant"}, []string{"mi:depleted_cell"})
	reactor.ItemOutputs[0].AmountNum = 2 // surplus per run: this is what makes the cycle solvable
	store := stubStoreFor([]*RecipeRow{
		stubRecipe("recipe:packager", "mi", "packager", 20,
			[]string{"mi:depleted_cell"}, []string{"mi:fuel_rod"}),
		reactor,
		stubRecipe("recipe:centrifuge", "mi", "centrifuge", 100,
			[]string{"mi:depleted_cell", "mi:water"}, []string{"mi:enriched_uranium"}),
	},
		&MachineSpec{ModID: "mi", MachineID: "packager"},
		&MachineSpec{ModID: "mi", MachineID: "reactor"},
		&MachineSpec{ModID: "mi", MachineID: "centrifuge"})

	tiny := plugins.Rational{Num: 1, Den: maxPluginRational}
	s := NewSolver(store, 1000)
	s.VariantSource = &overrideSource{
		rate: plugins.Rational{Num: 1, Den: 100},
		all:  &tiny,
		amounts: map[string]plugins.Rational{
			"mi:depleted_cell": {Num: 2, Den: maxPluginRational},
		},
	}

	_, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: ItemRef{ModID: "mi", ItemID: "fuel_rod"},
		TargetRate: NewRational(1, 1),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
	})
	if err == nil {
		t.Fatal("err = nil, want an arithmetic error from the linear system")
	}
	if !errors.Is(err, ErrRateOverflow) {
		t.Errorf("err = %v, want ErrRateOverflow", err)
	}
}

// TestSolveSurvivesOverflowInIntegerScaling covers ScaleToInteger: a legal
// variant rate of 1000/1 makes the machine counts fractional in thousandths, so
// the auto-mode LCM lifts every count by 1000 and a large host target overflows
// at the ExactCount scaling. safeLCMOfFractions only guards the LCM itself.
func TestSolveSurvivesOverflowInIntegerScaling(t *testing.T) {
	store, target := chainStub(3)
	s := NewSolver(store, 1000)
	s.VariantSource = &overrideSource{
		rate:  plugins.Rational{Num: 1, Den: 20},
		rates: map[string]plugins.Rational{"recipe:t1": {Num: 1000, Den: 1}},
	}

	_, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: target,
		TargetRate: NewRational(10000000000000001, 1),
		TimeUnit:   "t",
		Mode:       SolveModeAuto,
	})
	if err == nil {
		t.Fatal("err = nil, want an arithmetic error from the integer scaling")
	}
	if !errors.Is(err, ErrRateOverflow) {
		t.Errorf("err = %v, want ErrRateOverflow", err)
	}
}

// modSource answers per machine mod and records which config each mod was given.
type modSource struct {
	byMod   map[string][]plugins.Variant
	configs map[string]string
}

func (m *modSource) Variants(_ context.Context, machine *MachineSpec, _ *RecipeRow, config json.RawMessage) ([]plugins.Variant, error) {
	if m.configs == nil {
		m.configs = make(map[string]string)
	}
	m.configs[machine.ModID] = string(config)
	vs, ok := m.byMod[machine.ModID]
	if !ok {
		return nil, fmt.Errorf("no plugin for %s", machine.ModID)
	}
	return vs, nil
}

// groupFor looks a draft up by machine instead of by position: CalculateMachineGroups
// iterates a map, so the slice order is not stable.
func groupFor(groups []MachineGroupDraft, machineMod, machineID string) *MachineGroupDraft {
	for i := range groups {
		if groups[i].MachineMod == machineMod && groups[i].MachineID == machineID {
			return &groups[i]
		}
	}
	return nil
}

// TestSolveWithTwoModsKeepsVariantsAndCostsApart covers spec section 11: two
// machines from different mods in one solve, each evaluated by its own plugin
// under its own config, each keeping its own variant and its own cost resource.
func TestSolveWithTwoModsKeepsVariantsAndCostsApart(t *testing.T) {
	store := stubStoreFor([]*RecipeRow{
		stubRecipe("recipe:gear", "moda", "assembler", 20, []string{"mc:plate"}, []string{"mc:gear"}),
		stubRecipe("recipe:plate", "modb", "press", 20, []string{"mc:ore"}, []string{"mc:plate"}),
	},
		&MachineSpec{ModID: "moda", MachineID: "assembler"},
		&MachineSpec{ModID: "modb", MachineID: "press"})

	src := &modSource{byMod: map[string][]plugins.Variant{
		// Each mod's winner beats its sibling on machine count outright (1 against
		// 2, 2 against 4), so neither result depends on list order.
		"moda": {
			{ID: "a-base", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true},
			{ID: "a-fast", Rate: plugins.Rational{Num: 1, Den: 5}, Valid: true,
				Items: []plugins.Item{{Ref: "moda:upg", Count: 1}},
				Costs: []plugins.Cost{{Resource: "eu", Amount: plugins.Rational{Num: 128, Den: 1}}}},
		},
		"modb": {
			{ID: "b-slow", Rate: plugins.Rational{Num: 1, Den: 40}, Valid: true},
			{ID: "b-fast", Rate: plugins.Rational{Num: 1, Den: 20}, Valid: true,
				Items: []plugins.Item{{Ref: "modb:upg", Count: 1}},
				Costs: []plugins.Cost{{Resource: "mj", Amount: plugins.Rational{Num: 4, Den: 1}}}},
		},
	}}
	s := NewSolver(store, 1000)
	s.VariantSource = src

	res, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: ItemRef{ModID: "mc", ItemID: "gear"},
		TargetRate: NewRational(1, 10),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
		ModConfigs: map[string]json.RawMessage{
			"moda": json.RawMessage(`{"tier":"a"}`),
			"modb": json.RawMessage(`{"tier":"b"}`),
		},
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if len(res.MachineGroups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(res.MachineGroups))
	}

	a := groupFor(res.MachineGroups, "moda", "assembler")
	b := groupFor(res.MachineGroups, "modb", "press")
	if a == nil || b == nil {
		t.Fatalf("groups = %+v, want one per mod", res.MachineGroups)
	}
	if a.VariantID != "a-fast" {
		t.Errorf("moda VariantID = %q, want %q", a.VariantID, "a-fast")
	}
	if b.VariantID != "b-fast" {
		t.Errorf("modb VariantID = %q, want %q", b.VariantID, "b-fast")
	}
	if len(a.Costs) != 1 || a.Costs[0].Resource != "eu" || a.Costs[0].Amount.Num != 128 {
		t.Errorf("moda Costs = %+v, want one eu cost of 128", a.Costs)
	}
	if len(b.Costs) != 1 || b.Costs[0].Resource != "mj" || b.Costs[0].Amount.Num != 4 {
		t.Errorf("modb Costs = %+v, want one mj cost of 4", b.Costs)
	}
	if src.configs["moda"] != `{"tier":"a"}` || src.configs["modb"] != `{"tier":"b"}` {
		t.Errorf("configs = %v, want each mod its own config", src.configs)
	}
}

// TestCalculateMachineGroupsWarnsOncePerFailingMod: one broken plugin covers
// every group of its mod, so the result must not repeat the same warning per
// group.
func TestCalculateMachineGroupsWarnsOncePerFailingMod(t *testing.T) {
	store, target := chainStub(2)
	s := NewSolver(store, 1000)
	s.VariantSource = &stubSource{err: errors.New("plugin exploded")}

	res, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: target,
		TargetRate: NewRational(1, 20),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if len(res.MachineGroups) != 2 {
		t.Fatalf("want 2 groups from the two-stage chain, got %d", len(res.MachineGroups))
	}
	n := 0
	for _, w := range res.Warnings {
		if w.Code == "plugin_failed" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("plugin_failed warnings = %d, want 1 for two groups of the same mod", n)
	}
}

// TestMaxVariantIterationsIsPinned keeps the iteration budget from drifting
// unnoticed: every other test here measures against the constant, so changing it
// alone would leave the suite green.
func TestMaxVariantIterationsIsPinned(t *testing.T) {
	if MaxVariantIterations != 4 {
		t.Errorf("MaxVariantIterations = %d, want 4", MaxVariantIterations)
	}
}

// panicSource fails the way a host bug would, not the way rational.go does.
type panicSource struct{}

func (panicSource) Variants(context.Context, *MachineSpec, *RecipeRow, json.RawMessage) ([]plugins.Variant, error) {
	panic("boom")
}

// TestSolveDoesNotLaunderForeignPanics: Solve's guard covers rational.go's
// arithmetic, not every panic — a genuine bug must still surface as a crash
// instead of being reported as an unrepresentable rate.
func TestSolveDoesNotLaunderForeignPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("recover() = nil, want the foreign panic to propagate")
		}
		if msg, ok := r.(string); !ok || msg != "boom" {
			t.Errorf("recovered %v, want \"boom\"", r)
		}
	}()

	s := NewSolver(newStub(20), 1000)
	s.VariantSource = panicSource{}
	_, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: ItemRef{ModID: "minecraft", ItemID: "iron_ingot"},
		TargetRate: NewRational(1, 20),
		TimeUnit:   "t",
		Mode:       SolveModeTarget,
	})
	t.Fatalf("Solve returned (err = %v) instead of panicking", err)
}

// A pinned variant overrides the automatic pick even when it needs more
// machines - that is the entire point of pinning. Handoff section 3: the
// dominated upgrade step the solver would never choose has to be reachable.
func TestChooseVariantHonoursPin(t *testing.T) {
	vs := []plugins.Variant{
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 2}, Valid: true},
		{ID: "slow", Rate: plugins.Rational{Num: 1, Den: 4}, Valid: true,
			Items: []plugins.Item{{Ref: "mod:upgrade", Count: 3}}},
	}
	c, err := chooseVariant(vs, NewRational(1, 4), nil, "slow")
	if err != nil {
		t.Fatalf("chooseVariant: %v", err)
	}
	if c.variant.ID != "slow" {
		t.Errorf("variant = %q, want \"slow\"", c.variant.ID)
	}
	if !c.runnable {
		t.Error("runnable = false, want true for a valid pinned variant")
	}
	if c.count != 1 {
		t.Errorf("count = %d, want 1", c.count)
	}
}

// A pin naming a variant that is gone (the config changed under the client,
// or the plugin no longer emits it) falls back to the automatic pick rather
// than failing the solve. "worse" is listed first and is not the automatic
// pick (it needs 2 machines against base's 1): an implementation that
// mishandles an unmatched pin by defaulting to the first entry, rather than
// running the real automatic-pick logic, lands on "worse" and fails here.
func TestChooseVariantIgnoresUnknownPin(t *testing.T) {
	vs := []plugins.Variant{
		{ID: "worse", Rate: plugins.Rational{Num: 1, Den: 4}, Valid: true},
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 2}, Valid: true},
	}
	c, err := chooseVariant(vs, NewRational(1, 2), nil, "no_such_variant")
	if err != nil {
		t.Fatalf("chooseVariant: %v", err)
	}
	if c.variant.ID != "base" {
		t.Errorf("variant = %q, want the automatic pick %q", c.variant.ID, "base")
	}
	if c.count != 1 {
		t.Errorf("count = %d, want 1", c.count)
	}
}

// An invalid variant cannot be pinned: it is one the machine cannot run at
// all, and pinning it would report a machine count for something that never
// operates. A second valid variant, "fast", is the automatic pick (1 machine
// against base's 2) and also against "banned" if "banned" were honoured
// despite being invalid (2 machines, same rate as base) - so both the variant
// ID and the count distinguish "correctly fell back" from "pinned it anyway".
func TestChooseVariantIgnoresInvalidPin(t *testing.T) {
	vs := []plugins.Variant{
		{ID: "base", Rate: plugins.Rational{Num: 1, Den: 4}, Valid: true},
		{ID: "banned", Rate: plugins.Rational{Num: 1, Den: 4}, Valid: false},
		{ID: "fast", Rate: plugins.Rational{Num: 1, Den: 2}, Valid: true},
	}
	c, err := chooseVariant(vs, NewRational(1, 2), nil, "banned")
	if err != nil {
		t.Fatalf("chooseVariant: %v", err)
	}
	if c.variant.ID != "fast" {
		t.Errorf("variant = %q, want the automatic pick %q", c.variant.ID, "fast")
	}
	if c.count != 1 {
		t.Errorf("count = %d, want 1", c.count)
	}
}

// perRecipeSource answers each recipe with its own variant list, so a chain can
// hold one group that has an upgrade on offer and one that does not.
type perRecipeSource struct {
	vs map[string][]plugins.Variant
}

func (p *perRecipeSource) Variants(_ context.Context, _ *MachineSpec, r *RecipeRow, _ json.RawMessage) ([]plugins.Variant, error) {
	if vs, ok := p.vs[r.ID]; ok {
		return vs, nil
	}
	return []plugins.Variant{DefaultVariant(r)}, nil
}

// In AUTO mode the first variant pick sees the unscaled request rate, where
// every group is a fraction of one machine and no upgrade can pay off. Once the
// chain is scaled to whole machines (7 and 3 here) the pick must be repeated at
// the real rate: the 7-machine group collapses to 1 fast machine, the group
// without an upgrade stays as it was.
func TestSolveAutoRepicksVariantsAfterScaling(t *testing.T) {
	store, target := chainStub(2)
	src := &perRecipeSource{vs: map[string][]plugins.Variant{
		"recipe:t1": {
			{ID: "base", Rate: plugins.Rational{Num: 1, Den: 7}, Valid: true},
			{ID: "fast", Rate: plugins.Rational{Num: 1, Den: 1}, Valid: true,
				Items: []plugins.Item{{Ref: "m:upgrade", Count: 1}}},
		},
		"recipe:t2": {
			{ID: "base", Rate: plugins.Rational{Num: 1, Den: 3}, Valid: true},
		},
	}}
	s := NewSolver(store, 1000)
	s.VariantSource = src
	res, err := s.Solve(context.Background(), SolveRequest{
		TargetItem: target,
		TargetRate: NewRational(1, 100),
		TimeUnit:   "t",
		Mode:       SolveModeAuto,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	byRecipe := map[string]MachineGroupDraft{}
	for _, g := range res.MachineGroups {
		byRecipe[g.RecipeID] = g
	}
	t1, t2 := byRecipe["recipe:t1"], byRecipe["recipe:t2"]
	if t1.VariantID != "fast" || t1.Count != 1 {
		t.Errorf("t1 = %s x%d, want fast x1", t1.VariantID, t1.Count)
	}
	if t2.VariantID != "base" || t2.Count != 3 {
		t.Errorf("t2 = %s x%d, want base x3", t2.VariantID, t2.Count)
	}
	if got := res.ActualRate; got.Num != 1 || got.Den != 1 {
		t.Errorf("actual rate = %d/%d per tick, want 1/1: re-picking must keep the scaled rate", got.Num, got.Den)
	}
}
