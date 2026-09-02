package db

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

func TestVariantCacheHashIsStableAcrossKeyOrder(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"electric","mode":"auto"}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"mode":"auto","tier":"electric"}`))
	if a != b {
		t.Errorf("hash differs by key order: %s vs %s", a, b)
	}
}

func TestVariantCacheHashChangesWithVersion(t *testing.T) {
	if VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{}`)) == VariantCacheHash("2.0.0", nil, nil, json.RawMessage(`{}`)) {
		t.Error("hash must change when the plugin version changes")
	}
}

func TestVariantCacheHashChangesWithConfig(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"electric"}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"bronze"}`))
	if a == b {
		t.Error("hash must change when the config changes")
	}
}

func TestVariantCacheHashTreatsEmptyAsEmptyObject(t *testing.T) {
	if VariantCacheHash("1.0.0", nil, nil, nil) != VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{}`)) {
		t.Error("nil config must hash like an empty object")
	}
}

// Nested objects must canonicalize regardless of key order at every level,
// not just the top one.
func TestVariantCacheHashStableAcrossNestedKeyOrder(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"electric","limits":{"max":5,"min":1}}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"limits":{"min":1,"max":5},"tier":"electric"}`))
	if a != b {
		t.Errorf("hash differs by nested key order: %s vs %s", a, b)
	}
}

// Objects inside an array must also canonicalize by key, independent of each
// other and of the array's own position.
func TestVariantCacheHashStableAcrossKeyOrderInArrayElements(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"rules":[{"a":1,"b":2},{"c":3,"d":4}]}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"rules":[{"b":2,"a":1},{"d":4,"c":3}]}`))
	if a != b {
		t.Errorf("hash differs by key order inside array elements: %s vs %s", a, b)
	}
}

// Arrays are order-sensitive: reordering elements is a different config and
// must hash differently. Canonicalization must never touch array order.
func TestVariantCacheHashArrayElementOrderMatters(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"steps":["first","second"]}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"steps":["second","first"]}`))
	if a == b {
		t.Error("hash must change when array element order changes")
	}
}

// A JSON object must never hash the same as a JSON array of [key, value]
// pairs describing the same data. A canonicalizer that turns objects into
// ordered pairs to sort them risks exactly this collision, which would be
// worse than an unstable hash: two genuinely different configs would share a
// cache entry and the wrong variants would be served.
func TestVariantCacheHashObjectDoesNotCollideWithArrayOfPairs(t *testing.T) {
	obj := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"a":1,"b":2}`))
	arr := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`[["a",1],["b",2]]`))
	if obj == arr {
		t.Error("an object and an array-of-pairs describing it must not hash the same")
	}
}

// The whole no-invalidation design rests on a config change always landing
// on a different hash. json.Unmarshal into `any` decodes every number as
// float64 (53-bit mantissa), so two integers above 2^53 that differ only
// beyond that precision would otherwise collide onto the same key.
func TestVariantCacheHashDiffersForIntegersAboveFloat64Precision(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"x":9007199254740993}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"x":9007199254740992}`))
	if a == b {
		t.Error("hash must differ for integers that only differ above float64's 53-bit mantissa")
	}
}

// Replaces an earlier version of this test that only called VariantCacheHash
// twice with the identical input and compared the results: that proves
// nothing beyond "this function is deterministic", which holds trivially
// and would have passed even under the float64 bug this test exists to
// guard against. This version pins the actual property: two distinct
// values at the top of json.Number's range must still get distinct hashes,
// with call-to-call stability checked alongside as a secondary property.
func TestVariantCacheHashDistinguishesAndStabilizesValuesNearMaxInt64(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"x":9223372036854775807}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"x":9223372036854775806}`))
	if a == b {
		t.Error("hash must differ for distinct values near MaxInt64")
	}
	aAgain := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"x":9223372036854775807}`))
	if a != aAgain {
		t.Errorf("hash for the same value must be stable across calls: %s vs %s", a, aAgain)
	}
}

// canonicalJSON must reject trailing content after the first JSON value
// instead of silently canonicalizing only the leading document: a decoder
// that stops at the first value without checking for more would make
// `{"a":1}` and `{"a":1}{"b":2}` (or `{"a":1} trailing junk`) hash the same,
// even though they are different configs.
func TestVariantCacheHashRejectsTrailingContentAfterJSONValue(t *testing.T) {
	clean := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"a":1}`))
	concatenated := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"a":1}{"b":2}`))
	junk := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"a":1} trailing junk`))
	if clean == concatenated {
		t.Error("hash must differ when a second JSON value follows the first")
	}
	if clean == junk {
		t.Error("hash must differ when non-JSON content follows the first value")
	}
}

// Surrounding whitespace is not trailing content: a trailing newline or
// padding around the same single document must not change the hash.
func TestVariantCacheHashIgnoresSurroundingWhitespace(t *testing.T) {
	base := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"a":1}`))
	trailingNewline := VariantCacheHash("1.0.0", nil, nil, json.RawMessage("{\"a\":1}\n"))
	padded := VariantCacheHash("1.0.0", nil, nil, json.RawMessage("  {\"a\":1}  "))
	if base != trailingNewline {
		t.Error("a trailing newline must not change the hash")
	}
	if base != padded {
		t.Error("surrounding whitespace must not change the hash")
	}
}

// A top-level JSON array or scalar is a single valid document like an
// object is, and must still be canonicalized (re-encoded), not treated as
// unparsable and passed through raw.
func TestVariantCacheHashCanonicalizesTopLevelArrayAndScalar(t *testing.T) {
	if VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`[1,2]`)) != VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`[ 1 , 2 ]`)) {
		t.Error("a top-level array must canonicalize regardless of internal whitespace")
	}
	if VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`42`)) != VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`  42  `)) {
		t.Error("a top-level scalar must canonicalize regardless of surrounding whitespace")
	}
}

// Deliberate: 1, 1.0 and 1e0 are equal JSON numbers but different literals.
// Preserving numbers via json.Number keeps them at different hashes, trading
// a cache miss (recompute) for what would otherwise be a false hash match
// for integers above 2^53 (serving the wrong variants). This test pins that
// choice down so it isn't "cleaned up" later.
func TestVariantCacheHashDistinguishesNumericLiteralForms(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"x":1}`))
	b := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"x":1.0}`))
	if a == b {
		t.Error("want different hashes for the literal forms 1 and 1.0 (intentional, see comment)")
	}
}

func TestGetVariantsNotFound(t *testing.T) {
	d := testDB(t)
	if _, err := d.GetVariants(context.Background(), "absent", "absent", "00000000-0000-0000-0000-000000000000", "deadbeef"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// testVariants builds a variant fixture that exercises every field, with
// several distinct Rational pairs, so a lossy JSONB round trip would show up
// as a value mismatch rather than a length mismatch.
func testVariants() []plugins.Variant {
	return []plugins.Variant{
		{
			ID:    "auto",
			Label: "Auto",
			Rate:  plugins.Rational{Num: 7, Den: 3},
			Costs: []plugins.Cost{
				{Resource: "eu", Amount: plugins.Rational{Num: 32, Den: 1}},
				{Resource: "heat", Amount: plugins.Rational{Num: -1, Den: 5}},
			},
			Outputs: []plugins.Output{
				{Ref: "minecraft:iron_ingot", Amount: plugins.Rational{Num: 2, Den: 1}, Probability: plugins.Rational{Num: 1, Den: 1}},
				{Ref: "minecraft:nugget", Amount: plugins.Rational{Num: 1, Den: 1}, Probability: plugins.Rational{Num: 1, Den: 9223372036854775807}},
			},
			Items: []plugins.Item{
				{Ref: "gt:overclocker", Count: 4},
			},
			Valid: true,
		},
		{
			ID:      "eco",
			Label:   "Eco",
			Rate:    plugins.Rational{Num: -5, Den: 9223372036854775807},
			Costs:   nil,
			Outputs: nil,
			Items:   nil,
			Valid:   false,
		},
	}
}

func TestPutAndGetVariantsRoundTrip(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeID := seedRecipe(t, d, "testmod", "test_machine", "testmod")

	hash := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"electric"}`))
	in := testVariants()
	if err := d.PutVariants(ctx, "testmod", "test_machine", recipeID.String(), hash, in); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := d.GetVariants(ctx, "testmod", "test_machine", recipeID.String(), hash)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip changed values:\n got  = %+v\n want = %+v", got, in)
	}
}

func TestGetAnyBaseVariantCostsNotFound(t *testing.T) {
	d := testDB(t)
	if _, err := d.GetAnyBaseVariantCosts(context.Background(), "absent", "absent"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetAnyBaseVariantCostsReturnsNoItemsVariant(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeID := seedRecipe(t, d, "testmod", "test_machine", "testmod")

	hash := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{}`))
	fixture := testVariants()
	if err := d.PutVariants(ctx, "testmod", "test_machine", recipeID.String(), hash, fixture); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := d.GetAnyBaseVariantCosts(ctx, "testmod", "test_machine")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// fixture[0] ("auto") carries an installed item; fixture[1] ("eco") does
	// not, so it is the base variant whose costs must come back.
	want := fixture[1].Costs
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v (the no-items variant's costs)", got, want)
	}
}

// A machine can have several cached rows, some with an empty base-variant cost
// and one with a real one. The empty one must not shadow the real one.
func TestGetAnyBaseVariantCostsPrefersNonEmptyOverEmpty(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeA := seedRecipe(t, d, "testmod", "test_machine", "testmod")
	recipeB := seedRecipe(t, d, "testmod", "test_machine", "testmod")

	empty := []plugins.Variant{{ID: "base", Rate: plugins.Rational{Num: 1, Den: 1}, Valid: true}}
	real := []plugins.Variant{{
		ID: "base", Rate: plugins.Rational{Num: 1, Den: 2},
		Costs: []plugins.Cost{{Resource: "eu", Amount: plugins.Rational{Num: 128, Den: 1}}},
		Valid: true,
	}}
	hash := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{}`))
	// recipeA sorts before recipeB by UUID string only sometimes; try both
	// orderings by seeding whichever id is lexicographically smaller with
	// the empty-cost row, so the test doesn't depend on UUID generation luck.
	first, second := recipeA, recipeB
	if second.String() < first.String() {
		first, second = second, first
	}
	if err := d.PutVariants(ctx, "testmod", "test_machine", first.String(), hash, empty); err != nil {
		t.Fatalf("put empty: %v", err)
	}
	if err := d.PutVariants(ctx, "testmod", "test_machine", second.String(), hash, real); err != nil {
		t.Fatalf("put real: %v", err)
	}

	got, err := d.GetAnyBaseVariantCosts(ctx, "testmod", "test_machine")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, real[0].Costs) {
		t.Fatalf("got %+v, want the non-empty row's costs %+v", got, real[0].Costs)
	}
}

func TestGetAnyBaseVariantCostsSkipsRowWithoutABaseVariant(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeID := seedRecipe(t, d, "testmod", "test_machine", "testmod")

	// Every cached variant carries an installed item, so this row has no base variant.
	onlyUpgraded := []plugins.Variant{
		{ID: "loaded", Rate: plugins.Rational{Num: 1, Den: 1}, Items: []plugins.Item{{Ref: "x", Count: 1}}, Valid: true},
	}
	hash := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{}`))
	if err := d.PutVariants(ctx, "testmod", "test_machine", recipeID.String(), hash, onlyUpgraded); err != nil {
		t.Fatalf("put: %v", err)
	}

	if _, err := d.GetAnyBaseVariantCosts(ctx, "testmod", "test_machine"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound (no cached row has a no-items variant)", err)
	}
}

func TestPutVariantsReplacesExistingEntry(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeID := seedRecipe(t, d, "testmod", "test_machine", "testmod")

	hash := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"electric"}`))
	first := []plugins.Variant{{ID: "a", Label: "A", Rate: plugins.Rational{Num: 1, Den: 2}, Valid: true}}
	second := []plugins.Variant{{ID: "b", Label: "B", Rate: plugins.Rational{Num: 3, Den: 4}, Valid: true}}

	if err := d.PutVariants(ctx, "testmod", "test_machine", recipeID.String(), hash, first); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := d.PutVariants(ctx, "testmod", "test_machine", recipeID.String(), hash, second); err != nil {
		t.Fatalf("second put: %v", err)
	}

	got, err := d.GetVariants(ctx, "testmod", "test_machine", recipeID.String(), hash)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("got %+v, want the replaced value %+v", got, second)
	}
}

// A different config hash under the same (mod, machine, recipe) is a
// distinct cache slot; putting the new one must not disturb the old one.
func TestPutVariantsWithDifferentHashKeepsBothEntries(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeID := seedRecipe(t, d, "testmod", "test_machine", "testmod")

	hashA := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"electric"}`))
	hashB := VariantCacheHash("1.0.0", nil, nil, json.RawMessage(`{"tier":"bronze"}`))
	varsA := []plugins.Variant{{ID: "a", Label: "A", Rate: plugins.Rational{Num: 1, Den: 2}, Valid: true}}
	varsB := []plugins.Variant{{ID: "b", Label: "B", Rate: plugins.Rational{Num: 3, Den: 4}, Valid: true}}

	if err := d.PutVariants(ctx, "testmod", "test_machine", recipeID.String(), hashA, varsA); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := d.PutVariants(ctx, "testmod", "test_machine", recipeID.String(), hashB, varsB); err != nil {
		t.Fatalf("put b: %v", err)
	}

	gotA, err := d.GetVariants(ctx, "testmod", "test_machine", recipeID.String(), hashA)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if !reflect.DeepEqual(gotA, varsA) {
		t.Fatalf("entry a was disturbed: got %+v, want %+v", gotA, varsA)
	}

	gotB, err := d.GetVariants(ctx, "testmod", "test_machine", recipeID.String(), hashB)
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if !reflect.DeepEqual(gotB, varsB) {
		t.Fatalf("entry b: got %+v, want %+v", gotB, varsB)
	}
}

// Changed machine mod_data must land on a different cache key.
func TestVariantCacheHashChangesWithMachineModData(t *testing.T) {
	a := VariantCacheHash("1.0.0", json.RawMessage(`{"base_energy_per_tick":128}`), nil, nil)
	b := VariantCacheHash("1.0.0", json.RawMessage(`{"base_energy_per_tick":32}`), nil, nil)
	if a == b {
		t.Error("hash must change when the machine's mod_data changes")
	}
}

func TestVariantCacheHashChangesWithRecipeModData(t *testing.T) {
	a := VariantCacheHash("1.0.0", nil, json.RawMessage(`{"energy_per_tick":128}`), nil)
	b := VariantCacheHash("1.0.0", nil, json.RawMessage(`{"energy_per_tick":32}`), nil)
	if a == b {
		t.Error("hash must change when the recipe's mod_data changes")
	}
}

// The same blob in a different part must not produce the same digest.
func TestVariantCacheHashPartsDoNotBleedIntoEachOther(t *testing.T) {
	a := VariantCacheHash("1.0", json.RawMessage(`{"a":1}`), nil, nil)
	b := VariantCacheHash("1.0", nil, json.RawMessage(`{"a":1}`), nil)
	if a == b {
		t.Error("the same blob in a different position must hash differently")
	}
}

// Mod_data is canonicalized like the config.
func TestVariantCacheHashStableAcrossModDataKeyOrder(t *testing.T) {
	a := VariantCacheHash("1.0.0", json.RawMessage(`{"max":5,"min":1}`), nil, nil)
	b := VariantCacheHash("1.0.0", json.RawMessage(`{"min":1,"max":5}`), nil, nil)
	if a != b {
		t.Errorf("hash differs by mod_data key order: %s vs %s", a, b)
	}
}
