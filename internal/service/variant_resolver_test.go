package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// echoPlugin returns the recipe's outputs unchanged and runs at nominal speed.
const echoPlugin = `
var plugin = {
  api_version: 1,
  evaluate: function (ctx) {
    return [{
      id: "base", label: "Base",
      rate: { num: 1, den: ctx.recipe.duration_ticks },
      costs: [], outputs: ctx.recipe.outputs, items: [], valid: true
    }]
  }
}
`

// doublingPlugin echoes every output ref back with twice the amount, which is
// how a real mod expresses an output-boosting upgrade.
const doublingPlugin = `
var plugin = {
  api_version: 1,
  evaluate: function (ctx) {
    var outs = []
    for (var i = 0; i < ctx.recipe.outputs.length; i++) {
      var o = ctx.recipe.outputs[i]
      outs.push({ ref: o.ref,
                  amount: { num: o.amount.num * 2, den: o.amount.den },
                  probability: o.probability })
    }
    return [{
      id: "boosted", label: "Boosted",
      rate: { num: 1, den: ctx.recipe.duration_ticks },
      costs: [], outputs: outs, items: [], valid: true
    }]
  }
}
`

const throwingPlugin = `
var plugin = {
  api_version: 1,
  evaluate: function () { throw new Error("boom") }
}
`

// configReadingPlugin reads ctx.config.tier, which throws in JS if
// ctx.config is null or undefined rather than an object.
const configReadingPlugin = `
var plugin = {
  api_version: 1,
  evaluate: function (ctx) {
    var tier = ctx.config.tier
    return [{
      id: "base", label: "Base",
      rate: { num: 1, den: ctx.recipe.duration_ticks },
      costs: [], outputs: [], items: [], valid: true
    }]
  }
}
`

// stubStore is an in-memory variantStore. Each field controls one leg of the
// resolver: the plugin lookup, the cache read, and the cache write.
type stubStore struct {
	plugin    *db.ModPlugin
	pluginErr error
	cached    []plugins.Variant
	cacheErr  error
	putErr    error

	putCalls  int
	putKey    [4]string
	putStored []plugins.Variant
}

func (s *stubStore) GetModPlugin(_ context.Context, _ string) (*db.ModPlugin, error) {
	if s.pluginErr != nil {
		return nil, s.pluginErr
	}
	return s.plugin, nil
}

func (s *stubStore) GetVariants(_ context.Context, _, _, _, _ string) ([]plugins.Variant, error) {
	if s.cacheErr != nil {
		return nil, s.cacheErr
	}
	return s.cached, nil
}

func (s *stubStore) PutVariants(_ context.Context, modID, machineID, recipeID, configHash string, vs []plugins.Variant) error {
	s.putCalls++
	s.putKey = [4]string{modID, machineID, recipeID, configHash}
	s.putStored = vs
	if s.putErr != nil {
		return s.putErr
	}
	return nil
}

func modPlugin(source string) *db.ModPlugin {
	return &db.ModPlugin{ModID: "testmod", DisplayName: "Test Mod", Version: "1.0.0", APIVersion: 1, Source: source}
}

func testMachine() *solver.MachineSpec {
	return &solver.MachineSpec{ModID: "testmod", MachineID: "boiler", Name: "Boiler"}
}

// steamRecipe burns one coal into 100 mB of steam in 20 ticks. The output is a
// FLUID on purpose: its item key is "fluid:testmod:steam", the one ref shape a
// naive mod+":"+id concatenation gets wrong.
func steamRecipe() *solver.RecipeRow {
	coalMod, coalID := "testmod", "coal"
	return &solver.RecipeRow{
		ID:            "recipe:steam",
		MachineMod:    "testmod",
		MachineID:     "boiler",
		DurationTicks: 20,
		ItemInputs: []solver.RecipeRowItemIO{
			{ItemModID: &coalMod, ItemID: &coalID, AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
		},
		FluidOutputs: []solver.RecipeRowFluidIO{
			{FluidModID: "testmod", FluidID: "steam", AmountMB: 100, ProbabilityNum: 1, ProbabilityDen: 1},
		},
	}
}

func steamKey() string {
	ref := solver.ItemRef{ModID: "testmod", ItemID: "steam", IsFluid: true}
	return ref.Key()
}

// A mod without a plugin is not an error: it gets the host default variant.
func TestVariantsFallsBackToHostDefaultWithoutPlugin(t *testing.T) {
	r := newVariantResolver(&stubStore{pluginErr: db.ErrNotFound}, plugins.NewRegistry())

	vs, err := r.Variants(context.Background(), testMachine(), steamRecipe(), nil)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(vs) != 1 || vs[0].ID != "default" {
		t.Fatalf("got %+v, want the single host default variant", vs)
	}
	if vs[0].Rate != (plugins.Rational{Num: 1, Den: 20}) {
		t.Errorf("rate = %+v, want 1/20 (the recipe's nominal duration)", vs[0].Rate)
	}
}

// A failing plugin lookup must not read as "this mod has no plugin". Were it
// swallowed, a database outage would cost every machine at its nominal
// duration and report nothing.
func TestVariantsPropagatesPluginLookupFailure(t *testing.T) {
	boom := errors.New("connection refused")
	r := newVariantResolver(&stubStore{pluginErr: boom}, plugins.NewRegistry())

	vs, err := r.Variants(context.Background(), testMachine(), steamRecipe(), nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the store's error", err)
	}
	if vs != nil {
		t.Errorf("got variants %+v, want none alongside an error", vs)
	}
}

// Same for the cache read: only a miss may fall through to the plugin.
func TestVariantsPropagatesCacheReadFailure(t *testing.T) {
	boom := errors.New("relation does not exist")
	store := &stubStore{plugin: modPlugin(echoPlugin), cacheErr: boom}
	r := newVariantResolver(store, plugins.NewRegistry())

	if _, err := r.Variants(context.Background(), testMachine(), steamRecipe(), nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the store's error", err)
	}
	if store.putCalls != 0 {
		t.Errorf("PutVariants called %d times; a failed cache read must not reach the plugin", store.putCalls)
	}
}

// A cache hit is served as stored, without compiling or running the plugin.
func TestVariantsServesCacheHitWithoutRunningThePlugin(t *testing.T) {
	cached := []plugins.Variant{{ID: "cached", Label: "Cached", Rate: plugins.Rational{Num: 1, Den: 7}, Valid: true}}
	store := &stubStore{plugin: modPlugin(throwingPlugin), cached: cached}
	r := newVariantResolver(store, plugins.NewRegistry())

	vs, err := r.Variants(context.Background(), testMachine(), steamRecipe(), nil)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(vs) != 1 || vs[0].ID != "cached" {
		t.Fatalf("got %+v, want the cached entry", vs)
	}
	if store.putCalls != 0 {
		t.Errorf("PutVariants called %d times on a cache hit, want 0", store.putCalls)
	}
}

// A miss evaluates the plugin and writes the result under the key derived from
// the plugin version and the config.
func TestVariantsEvaluatesAndCachesOnMiss(t *testing.T) {
	store := &stubStore{plugin: modPlugin(echoPlugin), cacheErr: db.ErrNotFound}
	r := newVariantResolver(store, plugins.NewRegistry())
	config := json.RawMessage(`{"tier":"steel"}`)

	vs, err := r.Variants(context.Background(), testMachine(), steamRecipe(), config)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(vs) != 1 || vs[0].ID != "base" {
		t.Fatalf("got %+v, want the plugin's single variant", vs)
	}
	if store.putCalls != 1 {
		t.Fatalf("PutVariants called %d times, want 1", store.putCalls)
	}
	wantKey := [4]string{"testmod", "boiler", "recipe:steam", db.VariantCacheHash("1.0.0", testMachine().ModData, steamRecipe().ModData, config)}
	if store.putKey != wantKey {
		t.Errorf("cache key = %v, want %v", store.putKey, wantKey)
	}
	if !reflect.DeepEqual(store.putStored, vs) {
		t.Errorf("cached %+v, want the variants that were returned %+v", store.putStored, vs)
	}
}

// A broken plugin surfaces as an error. The resolver must not substitute the
// host default itself: the solver owns that degradation and counts one warning
// per mod while doing it.
func TestVariantsReturnsPluginFailureRatherThanDegrading(t *testing.T) {
	store := &stubStore{plugin: modPlugin(throwingPlugin), cacheErr: db.ErrNotFound}
	r := newVariantResolver(store, plugins.NewRegistry())

	vs, err := r.Variants(context.Background(), testMachine(), steamRecipe(), nil)
	if err == nil {
		t.Fatalf("got variants %+v, want an error from the throwing plugin", vs)
	}
	if vs != nil {
		t.Errorf("got variants %+v, want none alongside an error", vs)
	}
	if store.putCalls != 0 {
		t.Errorf("PutVariants called %d times after a plugin failure, want 0", store.putCalls)
	}
}

// The refs handed to a plugin are ItemRef keys. A fluid's key carries the
// "fluid:" prefix; getting this wrong raises no error anywhere, it just makes
// every amount override miss its node.
func TestRecipeOutputsUseItemRefKeys(t *testing.T) {
	recipe := steamRecipe()
	recipe.ItemOutputs = []solver.RecipeRowItemIO{
		{ItemModID: ptr("testmod"), ItemID: ptr("ash"), AmountNum: 1, AmountDen: 2, ProbabilityNum: 1, ProbabilityDen: 4},
	}

	outs := recipeOutputs(recipe)
	if len(outs) != 2 {
		t.Fatalf("got %d outputs, want 2", len(outs))
	}
	if outs[0].Ref != "testmod:ash" {
		t.Errorf("item ref = %q, want %q", outs[0].Ref, "testmod:ash")
	}
	if outs[0].Amount != (plugins.Rational{Num: 1, Den: 2}) || outs[0].Probability != (plugins.Rational{Num: 1, Den: 4}) {
		t.Errorf("item amount/probability = %+v / %+v, want 1/2 and 1/4 kept apart", outs[0].Amount, outs[0].Probability)
	}
	if outs[1].Ref != "fluid:testmod:steam" {
		t.Errorf("fluid ref = %q, want %q", outs[1].Ref, "fluid:testmod:steam")
	}
	if outs[1].Amount != (plugins.Rational{Num: 100, Den: 1}) {
		t.Errorf("fluid amount = %+v, want 100/1 mB", outs[1].Amount)
	}

	ins := recipeInputs(recipe)
	if len(ins) != 1 || ins[0].Ref != "testmod:coal" {
		t.Fatalf("inputs = %+v, want the single item input testmod:coal", ins)
	}
}

func TestRecipeInputsUseTagKeys(t *testing.T) {
	recipe := steamRecipe()
	recipe.ItemInputs = []solver.RecipeRowItemIO{
		{TagID: ptr("t1"), TagName: ptr("c:coals"), AmountNum: 1, AmountDen: 1, ProbabilityNum: 1, ProbabilityDen: 1},
	}
	ins := recipeInputs(recipe)
	if len(ins) != 1 || ins[0].Ref != "#c:coals" {
		t.Fatalf("inputs = %+v, want the tag key #c:coals", ins)
	}
}

// A caller with nothing configured passes nil, not "{}". db.VariantCacheHash
// canonicalizes both to the same cache key, so without normalizing the value
// handed to the plugin, whichever call happens to run first and fill the
// cache decides whether every later reader (including one that explicitly
// passed "{}") sees ctx.config as null or as an object.
func TestVariantsNormalizesNilConfigLikeEmptyObject(t *testing.T) {
	nilStore := &stubStore{plugin: modPlugin(configReadingPlugin), cacheErr: db.ErrNotFound}
	nilResolver := newVariantResolver(nilStore, plugins.NewRegistry())
	nilVs, err := nilResolver.Variants(context.Background(), testMachine(), steamRecipe(), nil)
	if err != nil {
		t.Fatalf("Variants with nil config: %v", err)
	}

	objStore := &stubStore{plugin: modPlugin(configReadingPlugin), cacheErr: db.ErrNotFound}
	objResolver := newVariantResolver(objStore, plugins.NewRegistry())
	objVs, err := objResolver.Variants(context.Background(), testMachine(), steamRecipe(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Variants with {} config: %v", err)
	}

	if !reflect.DeepEqual(nilVs, objVs) {
		t.Errorf("nil config = %+v, {} config = %+v, want identical", nilVs, objVs)
	}
	if nilStore.putKey != objStore.putKey {
		t.Errorf("cache key for nil config = %v, for {} config = %v, want identical", nilStore.putKey, objStore.putKey)
	}
}

// ── the whole chain ───────────────────────────────────────────────────────

// solveStore is the RecipeStore for the end-to-end test: one boiler recipe
// producing a fluid, with coal as a raw leaf.
type solveStore struct{ recipe *solver.RecipeRow }

func (s *solveStore) GetRecipesForItem(_ context.Context, _, _ string) ([]*solver.RecipeRow, error) {
	return nil, nil
}

func (s *solveStore) GetRecipesForFluid(_ context.Context, modID, fluidID string) ([]*solver.RecipeRow, error) {
	if modID == "testmod" && fluidID == "steam" {
		return []*solver.RecipeRow{s.recipe}, nil
	}
	return nil, nil
}

func (s *solveStore) GetRecipe(_ context.Context, id string) (*solver.RecipeRow, error) {
	if id == s.recipe.ID {
		return s.recipe, nil
	}
	return nil, nil
}

func (s *solveStore) GetMachineType(_ context.Context, modID, machineID string) (*solver.MachineSpec, error) {
	if modID == "testmod" && machineID == "boiler" {
		return testMachine(), nil
	}
	return nil, nil
}

func (s *solveStore) GetTagMembers(_ context.Context, _ string) ([]solver.ItemRef, error) {
	return nil, nil
}

// A plugin that doubles a FLUID output must change the solved machine count.
// This is the end of the chain the ref format runs through: recipeOutputs
// builds ctx.recipe.outputs, the plugin echoes those refs back, and the solver
// matches them against its graph nodes by exact string equality. Build the ref
// as mod+":"+id instead of ItemRef.Key() and every step still succeeds — the
// plugin validates, the variant is chosen, no warning is raised — while the
// doubled amount silently never reaches the graph.
func TestFluidOutputOverrideChangesTheSolvedMachineCount(t *testing.T) {
	recipe := steamRecipe()
	store := &solveStore{recipe: recipe}
	resolver := newVariantResolver(
		&stubStore{plugin: modPlugin(doublingPlugin), cacheErr: db.ErrNotFound},
		plugins.NewRegistry())

	sv := solver.NewSolver(store, 0)
	sv.VariantSource = resolver
	res, err := sv.Solve(context.Background(), solver.SolveRequest{
		TargetItem: solver.ItemRef{ModID: "testmod", ItemID: "steam", IsFluid: true},
		TargetRate: solver.NewRational(10, 1),
		TimeUnit:   "t",
		Mode:       solver.SolveModeTarget,
		// A fluid is a raw input unless a recipe is picked for it explicitly.
		RecipeOverrides: map[string]string{
			steamKey(): solver.RecipeOptionKey("recipe:steam", "testmod", "boiler"),
		},
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %+v, want none: a rejected variant would fake this test's result", res.Warnings)
	}
	if len(res.MachineGroups) != 1 {
		t.Fatalf("got %d machine groups, want 1", len(res.MachineGroups))
	}

	g := res.MachineGroups[0]
	if g.VariantID != "boosted" {
		t.Fatalf("variant = %q, want %q", g.VariantID, "boosted")
	}
	// 10 mB/t at 200 mB per craft is 1/20 crafts/t, which one boiler at
	// 1/20 crafts/t covers exactly. At the catalog's 100 mB it would take two.
	if g.Count != 1 {
		t.Errorf("machine count = %d, want 1 (2 means the doubled fluid output never reached the graph)", g.Count)
	}
	if !g.ExactCount.Eq(solver.NewRational(1, 1)) {
		t.Errorf("exact count = %+v, want 1/1", g.ExactCount)
	}

	// Half the crafts also means half the coal.
	var coal *solver.IOEntry
	for i := range res.IOProfile.Inputs {
		if res.IOProfile.Inputs[i].Item.Key() == "testmod:coal" {
			coal = &res.IOProfile.Inputs[i]
		}
	}
	if coal == nil {
		t.Fatalf("coal missing from the IO profile: %+v", res.IOProfile.Inputs)
	}
	if !coal.Rate.Eq(solver.NewRational(1, 20)) {
		t.Errorf("coal rate = %+v, want 1/20 per tick", coal.Rate)
	}

	// The override is only reachable through a ref the graph knows.
	if _, ok := solver.EffectiveOutputs(g.Variant)[steamKey()]; !ok {
		t.Errorf("variant outputs %+v carry no entry under %q", g.Variant.Outputs, steamKey())
	}
}

func ptr(s string) *string { return &s }

// The cache is an optimization: the variants are already computed and correct
// when the write is attempted. Failing the call would reach the solver as a
// plugin failure and degrade the whole mod to the host default.
func TestVariantsSurvivesACacheWriteFailure(t *testing.T) {
	store := &stubStore{
		plugin:   modPlugin(echoPlugin),
		cacheErr: db.ErrNotFound,
		putErr:   errors.New("connection reset by peer"),
	}
	r := newVariantResolver(store, plugins.NewRegistry())

	vs, err := r.Variants(context.Background(), testMachine(), steamRecipe(), nil)
	if err != nil {
		t.Fatalf("Variants returned %v; a cache write failure must not fail the call", err)
	}
	if len(vs) != 1 || vs[0].ID != "base" {
		t.Fatalf("got %+v, want the plugin's variant despite the failed write", vs)
	}
	if store.putCalls != 1 {
		t.Errorf("PutVariants called %d times, want 1", store.putCalls)
	}
}
