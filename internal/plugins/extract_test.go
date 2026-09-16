package plugins

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
)

// Nested arrays in Variant costs/outputs/items can each force huge
// allocations; extractVariants bounds each with its own length check and
// reads elements by index (never via Symbol.iterator) to prevent that.

// TestExtractRejectsOversizedNestedArrayWithoutAllocating asserts that a
// huge nested outputs array is rejected by its own length check before any
// per-element allocation, using a sparse array to isolate the check's cost.
func TestExtractRejectsOversizedNestedArrayWithoutAllocating(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var c = [];
			c.length = 30000000;
			return [{
				id: "v", label: "", rate: { num: 1, den: 1 },
				costs: [], outputs: c, items: [], valid: true
			}];
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	_, err = prog.Evaluate(context.Background(), sampleContext())

	runtime.ReadMemStats(&after)
	delta := after.TotalAlloc - before.TotalAlloc

	if err == nil {
		t.Fatal("want error for a variant with an oversized nested array, got nil")
	}
	const ceiling = 1 << 20 // 1 MiB
	if delta > ceiling {
		t.Fatalf("rejecting a 30000000-element outputs array allocated %d bytes, want under %d", delta, ceiling)
	}
	t.Logf("oversized nested array rejection: err=%v allocated=%d bytes", err, delta)
}

// TestExtractRejects256VariantsWithLargeCosts asserts the same bound holds
// inside the outer loop: 256 variants (at the outer limit) each carrying an
// oversized costs array must be rejected without allocating for any.
func TestExtractRejects256VariantsWithLargeCosts(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var out = [];
			for (var i = 0; i < 256; i++) {
				var c = [];
				c.length = 200000;
				out.push({
					id: "v" + i, label: "", rate: { num: 1, den: 1 },
					costs: c, outputs: [], items: [], valid: true
				});
			}
			return out;
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	_, err = prog.Evaluate(context.Background(), sampleContext())

	runtime.ReadMemStats(&after)
	delta := after.TotalAlloc - before.TotalAlloc

	if err == nil {
		t.Fatal("want error for 256 variants each with an oversized costs array, got nil")
	}
	// Building 256 outer objects costs real allocation; the ceiling sits
	// well clear of exporting 256 x 200000 costs elements (1.64 GB).
	const ceiling = 4 << 20 // 4 MiB
	if delta > ceiling {
		t.Fatalf("rejecting 256 oversized-costs variants allocated %d bytes, want under %d", delta, ceiling)
	}
	t.Logf("256-variants-large-costs rejection: err=%v allocated=%d bytes", err, delta)
}

// TestExtractIgnoresOverriddenSymbolIterator asserts that a genuine return
// array (ClassName() == "Array", a real, small length) whose Symbol.iterator
// has been overridden to yield unlimited elements is still read correctly:
// extractVariants reads elements strictly by index (arr.Get("0"), ...),
// which never invokes the iterator protocol.
func TestExtractIgnoresOverriddenSymbolIterator(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var arr = [{
				id: "ok", label: "", rate: { num: 1, den: 1 },
				costs: [], outputs: [], items: [], valid: true
			}];
			arr[Symbol.iterator] = function () {
				return {
					next: function () {
						return { done: false, value: {
							id: "evil", label: "", rate: { num: 1, den: 1 },
							costs: [], outputs: [], items: [], valid: true
						} };
					}
				};
			};
			return arr;
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	got, err := prog.Evaluate(context.Background(), sampleContext())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("got %+v, want exactly one variant with ID %q (the overridden iterator must have no effect on index-based extraction)", got, "ok")
	}
}

// TestExtractRejectsWrongFieldType asserts that a field present with the
// wrong JS type is an error, never a silently-defaulted zero value: rate
// must be an object with numeric num/den, and a string in its place must
// not become a zero Rational.
func TestExtractRejectsWrongFieldType(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			return [{
				id: "v", label: "", rate: "schnell",
				costs: [], outputs: [], items: [], valid: true
			}];
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if _, err := prog.Evaluate(context.Background(), sampleContext()); err == nil {
		t.Fatal("want error for rate given as a string instead of an object, got nil")
	}
}

// Extracted strings must be length-bounded, and readInt must reject
// non-exact integers instead of truncating or saturating.

// evaluateVariantFields compiles a plugin returning a single variant whose
// body is the given JS object literal fields, and evaluates it once.
func evaluateVariantFields(t *testing.T, fields string) ([]Variant, error) {
	t.Helper()
	prog, err := Compile("testmod", `var plugin = { evaluate: function (ctx) { return [{ `+fields+` }]; } }`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return prog.Evaluate(context.Background(), sampleContext())
}

// TestExtractRejectsOversizedStringWithoutCopying asserts that a single
// oversized string is rejected on its UTF-16 length before any UTF-8 copy
// is made. goja rebuilds a fresh UTF-8 copy on every String() call; at the
// 256-variant / 64-element caps, 49664 reads of a 100000-character string
// could allocate 9.18 GB, with copying on the Go side beyond interrupt reach.
func TestExtractRejectsOversizedStringWithoutCopying(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var big = "ä".repeat(100000);
			var out = [];
			for (var i = 0; i < 64; i++) {
				var costs = [];
				for (var j = 0; j < 64; j++) {
					costs.push({ resource: big, amount: { num: 1, den: 1 } });
				}
				out.push({
					id: "v" + i, label: "", rate: { num: 1, den: 1 },
					costs: costs, outputs: [], items: [], valid: true
				});
			}
			return out;
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	start := time.Now()
	_, err = prog.Evaluate(context.Background(), sampleContext())
	elapsed := time.Since(start)

	runtime.ReadMemStats(&after)
	delta := after.TotalAlloc - before.TotalAlloc
	t.Logf("oversized-string rejection: err=%v elapsed=%v allocated=%d bytes", err, elapsed, delta)

	if err == nil {
		t.Fatal("want error for a 100000-character string in costs[].resource, got nil")
	}
	// Naming the per-string cap specifically: the total byte budget would
	// also stop this fixture eventually, after copying a few megabytes, so
	// only this assertion pins down that the length was rejected up front.
	if !strings.Contains(err.Error(), "characters long, limit is") {
		t.Fatalf("got %v, want the per-string length cap to be what rejects this", err)
	}
	// Ceiling calibrated against the 9.18 GB without the length check;
	// legitimate allocation (64x64 costs + 200 KB string) is a few MB.
	const ceiling = 64 << 20
	if delta > ceiling {
		t.Fatalf("rejecting a 100000-character string allocated %d bytes, want under %d (the string may be getting copied before its length is checked)", delta, ceiling)
	}
	if elapsed > evalTimeout {
		t.Fatalf("rejecting a 100000-character string took %v, want well under evalTimeout=%v", elapsed, evalTimeout)
	}
}

// TestExtractRejectsTotalStringBudget asserts the second half of the same
// bound: strings that each pass the per-string cap must still not add up
// without limit across one evaluate call. Every string here is 200
// characters (under maxStringLen=256), but 256 variants x 64 costs is
// 16384 of them, about 9.8 MB of UTF-8 - past maxStringBytes. Both the
// variant count and the costs count sit exactly at their documented caps,
// so nothing but the byte budget can reject this.
func TestExtractRejectsTotalStringBudget(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var s = "ä".repeat(200);
			var out = [];
			for (var i = 0; i < 256; i++) {
				var costs = [];
				for (var j = 0; j < 64; j++) {
					costs.push({ resource: s, amount: { num: 1, den: 1 } });
				}
				out.push({
					id: "v" + i, label: "", rate: { num: 1, den: 1 },
					costs: costs, outputs: [], items: [], valid: true
				});
			}
			return out;
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = prog.Evaluate(context.Background(), sampleContext())
	if err == nil {
		t.Fatal("want error for 16384 strings of 200 characters each, got nil")
	}
	if !strings.Contains(err.Error(), "bytes of strings in one evaluate call") {
		t.Fatalf("got %v, want the total string budget to be what rejects this (every single string is within maxStringLen)", err)
	}
	t.Logf("string-budget rejection: err=%v", err)
}

// TestExtractRejectsNonIntegerNumbers asserts that every number feeding a
// Rational or an Item count is an exact, finite integer within int64 range.
// goja's ToInteger() silently truncates a fraction, maps NaN to 0 and
// saturates +/-Infinity and out-of-range values to the int64 extremes, so
// an honest arithmetic slip such as num: total/duration would silently
// become a zero rate with err == nil, and per spec section 5.4 that result
// is cached in Postgres, outliving the restart.
func TestExtractRejectsNonIntegerNumbers(t *testing.T) {
	base := `id: "v", label: "", costs: [], outputs: [], items: [], valid: true, `
	rejected := []struct{ name, fields string }{
		{"fractional num", base + `rate: { num: 1.5, den: 1 }`},
		{"num below one truncating to zero", base + `rate: { num: 0.9, den: 1 }`},
		{"negative fractional num", base + `rate: { num: -1.5, den: 1 }`},
		{"NaN num", base + `rate: { num: NaN, den: 1 }`},
		{"positive infinity num", base + `rate: { num: Infinity, den: 1 }`},
		{"negative infinity num", base + `rate: { num: -Infinity, den: 1 }`},
		{"num far beyond int64", base + `rate: { num: 1e30, den: 1 }`},
		{"num at 2^63", base + `rate: { num: Math.pow(2, 63), den: 1 }`},
		{"num below int64 minimum", base + `rate: { num: -1e30, den: 1 }`},
		{"fractional den", base + `rate: { num: 1, den: 1.5 }`},
		{"fractional amount in a cost", `id: "v", label: "", rate: { num: 1, den: 1 }, outputs: [], items: [], valid: true,
			costs: [{ resource: "eu", amount: { num: 0.5, den: 1 } }]`},
		{"fractional item count", `id: "v", label: "", rate: { num: 1, den: 1 }, costs: [], outputs: [], valid: true,
			items: [{ ref: "mi:advanced_upgrade", count: 2.5 }]`},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluateVariantFields(t, tc.fields)
			if err == nil {
				t.Fatalf("got %+v with err == nil, want a validation error naming the field and the value seen", got)
			}
			t.Logf("%v", err)
		})
	}
}

// minimalVariantFields is the field set every case below adds a rate to: an
// otherwise-empty, otherwise-valid single variant.
const minimalVariantFields = `id: "v", label: "", costs: [], outputs: [], items: [], valid: true, `

// TestExtractAcceptsExactIntegers asserts extractVariants' own exactness
// rule directly, bypassing Evaluate/Validate: any exact int64 - fractional,
// negative, zero, or large but still exact - is a fine number at the
// extraction layer, whatever the plugin contract separately makes of a zero
// or oversized rate (see TestValidateRejectsZeroRate and
// TestValidateRejectsOversizedMagnitude in validate_test.go). All four cases
// go through extractVariants the same way, so this file uses one convention
// throughout for testing extraction rather than mixing a full-pipeline path
// with a direct one.
func TestExtractAcceptsExactIntegers(t *testing.T) {
	cases := []struct {
		name   string
		fields string
		want   Rational
	}{
		{"exact fraction", minimalVariantFields + `rate: { num: 1, den: 176 }`, Rational{1, 176}},
		{"zero num", minimalVariantFields + `rate: { num: 0, den: 1 }`, Rational{0, 1}},
		{"negative num", minimalVariantFields + `rate: { num: -3, den: 20 }`, Rational{-3, 20}},
		{"large but exact num", minimalVariantFields + `rate: { num: 1e18, den: 1 }`, Rational{1000000000000000000, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := goja.New()
			res, err := vm.RunString(`[{ ` + tc.fields + ` }]`)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			got, err := extractVariants("testmod", res)
			if err != nil {
				t.Fatalf("extractVariants: %v", err)
			}
			if len(got) != 1 || got[0].Rate != tc.want {
				t.Fatalf("got %+v, want one variant with rate %+v", got, tc.want)
			}
		})
	}
}

// TestExtractRejectsZeroDenominator asserts that a present rational with a
// zero denominator is a validation error here. It currently flows through
// without consequence, but becomes an integer divide by zero - a panic
// outside guard()'s reach - as soon as the solver computes with it.
func TestExtractRejectsZeroDenominator(t *testing.T) {
	base := `id: "v", label: "", costs: [], outputs: [], items: [], valid: true, `
	cases := []struct{ name, fields string }{
		{"explicit zero den", base + `rate: { num: 1, den: 0 }`},
		{"omitted den", base + `rate: { num: 1 }`},
		{"zero den in a cost amount", `id: "v", label: "", rate: { num: 1, den: 1 }, outputs: [], items: [], valid: true,
			costs: [{ resource: "eu", amount: { num: 176, den: 0 } }]`},
		{"zero den in an output probability", `id: "v", label: "", rate: { num: 1, den: 1 }, costs: [], items: [], valid: true,
			outputs: [{ ref: "mi:bronze_dust", amount: { num: 2, den: 1 }, probability: { num: 1, den: 0 } }]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluateVariantFields(t, tc.fields)
			if err == nil {
				t.Fatalf("got %+v with err == nil, want den == 0 to be a validation error", got)
			}
			if !strings.Contains(err.Error(), "den must not be zero") {
				t.Fatalf("got %v, want an error naming the zero denominator", err)
			}
			t.Logf("%v", err)
		})
	}
}

func TestExtractReadsRank(t *testing.T) {
	cases := []struct {
		name   string
		fields string
		want   int
	}{
		{"absent", minimalVariantFields + `rate: { num: 1, den: 20 }`, 0},
		{"set", minimalVariantFields + `rate: { num: 1, den: 20 }, rank: 3`, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := goja.New()
			res, err := vm.RunString(`[{ ` + tc.fields + ` }]`)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			got, err := extractVariants("testmod", res)
			if err != nil {
				t.Fatalf("extractVariants: %v", err)
			}
			if len(got) != 1 || got[0].Rank != tc.want {
				t.Fatalf("got %+v, want one variant with rank %d", got, tc.want)
			}
		})
	}
}
