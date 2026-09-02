package plugins

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestEvaluateRejectsChangedOutputSet(t *testing.T) {
	prog := loadFixture(t, "bad_output_set.js")
	_, err := prog.Evaluate(context.Background(), sampleContext())
	if err == nil {
		t.Fatal("want error for changed output set, got nil")
	}
	if !strings.Contains(err.Error(), "output set") {
		t.Errorf("error = %q, want it to mention the output set", err)
	}
}

func TestEvaluateRejectsMissingBaseVariant(t *testing.T) {
	prog := loadFixture(t, "no_base.js")
	_, err := prog.Evaluate(context.Background(), sampleContext())
	if err == nil {
		t.Fatal("want error for missing base variant, got nil")
	}
	if !strings.Contains(err.Error(), "base variant") {
		t.Errorf("error = %q, want it to mention the base variant", err)
	}
}

func TestEvaluatePropagatesPluginException(t *testing.T) {
	prog := loadFixture(t, "throws.js")
	if _, err := prog.Evaluate(context.Background(), sampleContext()); err == nil {
		t.Fatal("want error from throwing plugin, got nil")
	}
}

// TestValidateRejectsZeroDenominator asserts that a present numerator paired
// with a zero denominator (Rational{1, 0}) is rejected as a zero
// denominator specifically, not conflated with a wholly missing field
// (Rational{0, 0}, checked separately by TestValidateRejectsMissingField).
// Evaluate's own pipeline never reaches this branch - extract.go's
// readRational rejects a present Den == 0 first, with its own message - but
// Validate is called directly here, so this exercises checkRational's own
// fallback for that case.
func TestValidateRejectsZeroDenominator(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: 1, Den: 0}, Outputs: ec.Recipe.Outputs, Valid: true}}
	err := Validate(ec, vs)
	if err == nil {
		t.Fatal("want error for zero denominator, got nil")
	}
	if !strings.Contains(err.Error(), "zero denominator") {
		t.Fatalf("got %v, want the zero-denominator message specifically, not the missing-field one", err)
	}
}

// TestValidateRejectsMissingField asserts that a wholly missing field -
// Rational{0, 0}, exactly what extract.go's readRational returns for a
// field the plugin never wrote at all - is reported as missing, not as a
// zero denominator.
func TestValidateRejectsMissingField(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{}, Outputs: ec.Recipe.Outputs, Valid: true}}
	err := Validate(ec, vs)
	if err == nil {
		t.Fatal("want error for a missing field, got nil")
	}
	if !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("got %v, want the missing-field message specifically, not the zero-denominator one", err)
	}
}

// TestValidateRejectsEmptyResult pins down the len(vs) == 0 check
// specifically, not just "some error". Without that check, Validate(ec, nil)
// still returns an error - the empty loop leaves bases at 0, and the
// bases != 1 check at the end catches that incidentally - so a test that
// only asserts err != nil cannot tell the two checks apart and would stay
// green even if the empty-result check itself were deleted.
func TestValidateRejectsEmptyResult(t *testing.T) {
	err := Validate(sampleContext(), nil)
	if err == nil {
		t.Fatal("want error for empty variant list, got nil")
	}
	if !strings.Contains(err.Error(), "evaluate returned no variants") {
		t.Fatalf("got %v, want the empty-result check specifically (not the base-variant count catching this incidentally)", err)
	}
}

func TestValidateAcceptsChangedAmounts(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{
		{ID: "base", Rate: Rational{1, 20}, Outputs: ec.Recipe.Outputs, Valid: true},
		{
			ID: "double", Rate: Rational{1, 20}, Valid: true,
			Items:   []Item{{Ref: "testmod:upgrade", Count: 1}},
			Outputs: []Output{{Ref: "testmod:dust", Amount: Rational{2, 1}, Probability: Rational{1, 1}}},
		},
	}
	if err := Validate(ec, vs); err != nil {
		t.Fatalf("changed amounts must be allowed: %v", err)
	}
}

// TestValidateDefaultsMissingProbability asserts that a wholly missing
// probability field - which extracts as Rational{0, 0} (extract.go's
// readRational) - is defaulted to 1/1 rather than rejected, since a
// deterministic output is the common case.
func TestValidateDefaultsMissingProbability(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: []Output{{Ref: "testmod:dust", Amount: Rational{1, 1}}},
	}}
	if err := Validate(ec, vs); err != nil {
		t.Fatalf("a missing probability must default, not fail: %v", err)
	}
	if got := vs[0].Outputs[0].Probability; got != (Rational{Num: 1, Den: 1}) {
		t.Fatalf("Probability = %+v, want the 1/1 default", got)
	}
}

// TestValidateDefaultsMissingProbabilityWithoutMutatingSharedSlice asserts
// that defaulting a missing probability writes into a copy of Outputs, not
// the slice the caller passed in. Go callers commonly build
// Variant{Outputs: ec.Recipe.Outputs}, sharing the recipe's own backing
// array; mutating it in place would leak the default into the recipe
// itself, which every other variant and every future call sees.
func TestValidateDefaultsMissingProbabilityWithoutMutatingSharedSlice(t *testing.T) {
	ec := sampleContext()
	ec.Recipe.Outputs = []Output{{Ref: "testmod:dust", Amount: Rational{1, 1}}} // no Probability -> zero value
	vs := []Variant{{ID: "base", Rate: Rational{1, 20}, Valid: true, Outputs: ec.Recipe.Outputs}}

	if err := Validate(ec, vs); err != nil {
		t.Fatalf("missing probability must default, not fail: %v", err)
	}
	if got := vs[0].Outputs[0].Probability; got != (Rational{Num: 1, Den: 1}) {
		t.Fatalf("Probability = %+v, want the 1/1 default on the returned variant", got)
	}
	if got := ec.Recipe.Outputs[0].Probability; got != (Rational{}) {
		t.Fatalf("Recipe.Outputs[0].Probability = %+v, want the caller's shared slice left untouched", got)
	}
}

// TestValidateRejectsNegativeDenominator asserts that a negative
// denominator such as -20 is rejected: rational.go's normPos and cmpCross
// assume Den > 0 throughout, and the extraction layer only rejects
// Den == 0, not a negative Den.
func TestValidateRejectsNegativeDenominator(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: 1, Den: -20}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a negative denominator, got nil")
	}
}

// TestValidateRejectsOversizedMagnitude asserts that a numerator one past
// maxPluginMagnitude is rejected: the extraction layer lets any exact int64
// through, but rational.go's smul/sadd panic on overflow, so this bound
// must be enforced here, before the value can reach the solver.
func TestValidateRejectsOversizedMagnitude(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: maxPluginMagnitude + 1, Den: 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a numerator beyond the plugin magnitude bound, got nil")
	}
}

// TestValidateRejectsOversizedDenominator is the Den-side counterpart of
// TestValidateRejectsOversizedMagnitude: checkRational bounds Num and Den
// with two separate comparisons, and a numerator-only test does not exercise
// the Den branch at all.
func TestValidateRejectsOversizedDenominator(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: 1, Den: maxPluginMagnitude + 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a denominator beyond the plugin magnitude bound, got nil")
	}
}

// TestValidateRejectsMinInt64 asserts that math.MinInt64 is rejected:
// NewRational in rational.go explicitly panics on it, and the extraction
// layer lets it through unmodified since it is an exact, in-range int64.
func TestValidateRejectsMinInt64(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: math.MinInt64, Den: 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for math.MinInt64, got nil")
	}
}

// TestValidateRejectsZeroRate asserts that rate == 0 is rejected: it is
// meaningless as recipes per tick, and solver.Rational.Div panics on a
// zero-numerator divisor the same way it does on Den == 0 - the same panic
// class Den == 0 is rejected for. amount and probability are deliberately
// not held to this rule; see TestValidateAcceptsZeroAmountAndProbability.
func TestValidateRejectsZeroRate(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: 0, Den: 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a zero rate, got nil")
	}
}

// TestValidateAcceptsZeroAmountAndProbability asserts the other side of I4:
// checkRational's allowZero only applies to rate. A zero output amount or
// probability is meaningful ("this output does not occur in this variant")
// and the solver computes with it cleanly, so it must not be rejected.
func TestValidateAcceptsZeroAmountAndProbability(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: []Output{{Ref: "testmod:dust", Amount: Rational{0, 1}, Probability: Rational{0, 1}}},
	}}
	if err := Validate(ec, vs); err != nil {
		t.Fatalf("a zero output amount/probability must be allowed: %v", err)
	}
}

// TestValidateRejectsProbabilityAboveOne asserts that a probability above 1
// is rejected: it is not a probability. A variant that produces more than
// one unit on average belongs in amount instead, at probability 1/1.
func TestValidateRejectsProbabilityAboveOne(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: []Output{{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{7, 1}}},
	}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for an output probability greater than 1, got nil")
	}
}

// TestValidateRejectsEmptyID and TestValidateRejectsDuplicateID assert that
// an empty variant id and a duplicate variant id are each rejected on their
// own: neither check is exercised by any other test in this file.
func TestValidateRejectsEmptyID(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "", Rate: Rational{1, 20}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for an empty variant id, got nil")
	}
}

func TestValidateRejectsDuplicateID(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{
		{ID: "base", Rate: Rational{1, 20}, Outputs: ec.Recipe.Outputs, Valid: true},
		{ID: "base", Rate: Rational{1, 10}, Valid: true,
			Items:   []Item{{Ref: "testmod:upgrade", Count: 1}},
			Outputs: ec.Recipe.Outputs,
		},
	}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a duplicate variant id, got nil")
	}
}

// TestValidateRejectsEmptyCostResource asserts that a cost with an empty
// resource name is rejected.
func TestValidateRejectsEmptyCostResource(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Costs:   []Cost{{Resource: "", Amount: Rational{1, 1}}},
		Outputs: ec.Recipe.Outputs,
	}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a cost with an empty resource, got nil")
	}
}

// TestValidateRejectsNonPositiveItemCount and
// TestValidateRejectsOversizedItemCount assert that an installed item's
// count must be positive and within the plugin magnitude bound. Both tests
// include a separate, valid base variant so bases == 1 is already
// satisfied: without one, removing the item-count check under test would
// still leave the "no base variant" check to fail the test for an
// unrelated reason - the same confound TestValidateRejectsEmptyResult
// isolates against for the empty-result check.
func TestValidateRejectsNonPositiveItemCount(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{
		{ID: "base", Rate: Rational{1, 20}, Outputs: ec.Recipe.Outputs, Valid: true},
		{
			ID: "upgraded", Rate: Rational{1, 20}, Valid: true,
			Items:   []Item{{Ref: "testmod:upgrade", Count: 0}},
			Outputs: ec.Recipe.Outputs,
		},
	}
	err := Validate(ec, vs)
	if err == nil {
		t.Fatal("want error for a non-positive item count, got nil")
	}
	if !strings.Contains(err.Error(), "installs") || !strings.Contains(err.Error(), "count 0") {
		t.Fatalf("got %v, want the item-count check specifically", err)
	}
}

func TestValidateRejectsOversizedItemCount(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{
		{ID: "base", Rate: Rational{1, 20}, Outputs: ec.Recipe.Outputs, Valid: true},
		{
			ID: "upgraded", Rate: Rational{1, 20}, Valid: true,
			Items:   []Item{{Ref: "testmod:upgrade", Count: maxPluginMagnitude + 1}},
			Outputs: ec.Recipe.Outputs,
		},
	}
	err := Validate(ec, vs)
	if err == nil {
		t.Fatal("want error for an item count beyond the plugin magnitude bound, got nil")
	}
	if !strings.Contains(err.Error(), "beyond the plugin magnitude bound") {
		t.Fatalf("got %v, want the item-count magnitude check specifically", err)
	}
}

// TestValidateRejectsBadCostAmount, TestValidateRejectsBadOutputAmount and
// TestValidateRejectsBadOutputProbability assert each of checkRational's
// three other call sites independently: cost amount, output amount and
// output probability all reuse checkRational, and each needs its own bad
// value to exercise that call site specifically.
func TestValidateRejectsBadCostAmount(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Costs:   []Cost{{Resource: "eu", Amount: Rational{Num: -1, Den: 1}}},
		Outputs: ec.Recipe.Outputs,
	}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a negative cost amount, got nil")
	}
}

func TestValidateRejectsBadOutputAmount(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: []Output{{Ref: "testmod:dust", Amount: Rational{Num: -1, Den: 1}, Probability: Rational{1, 1}}},
	}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a negative output amount, got nil")
	}
}

func TestValidateRejectsBadOutputProbability(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: []Output{{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{Num: -1, Den: 1}}},
	}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a negative output probability, got nil")
	}
}

// TestValidateRejectsUnknownOutputRef isolates the per-output membership
// check (want[o.Ref] == 0) from the multiset count comparison that follows
// it: the recipe's one required ref is present at the right count, so the
// count comparison alone would not catch an extra, unrelated ref appended
// alongside it - only the membership check does.
func TestValidateRejectsUnknownOutputRef(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: []Output{
			{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{1, 1}},
			{Ref: "testmod:unknown", Amount: Rational{1, 1}, Probability: Rational{1, 1}},
		},
	}}
	err := Validate(ec, vs)
	if err == nil {
		t.Fatal("want error for an output ref the recipe does not have, got nil")
	}
	if !strings.Contains(err.Error(), "is not a recipe output") {
		t.Fatalf("got %v, want the unknown-ref check specifically", err)
	}
}

// TestValidateRejectsOutputMultisetMismatch asserts that Validate rejects a
// variant that drops one recipe output and duplicates another at the same
// total count (dust, slag -> dust, dust): the output set is checked as a
// multiset per ref, not merely by total length.
func TestValidateRejectsOutputMultisetMismatch(t *testing.T) {
	ec := sampleContext()
	ec.Recipe.Outputs = []Output{
		{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{1, 1}},
		{Ref: "testmod:slag", Amount: Rational{1, 1}, Probability: Rational{1, 1}},
	}
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: []Output{
			{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{1, 1}},
			{Ref: "testmod:dust", Amount: Rational{5, 1}, Probability: Rational{1, 1}},
		},
	}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a variant that drops one recipe output and duplicates another, got nil")
	}
}

// TestValidateAcceptsDuplicateRecipeOutputsEchoedBack asserts the reverse
// case: a recipe that itself repeats an output ref has want[ref] > 1, and a
// variant echoing ctx.recipe.outputs back verbatim must still be accepted -
// a plain set comparison with len(Recipe.Outputs) would undercount a
// repeated ref and wrongly reject this.
func TestValidateAcceptsDuplicateRecipeOutputsEchoedBack(t *testing.T) {
	ec := sampleContext()
	ec.Recipe.Outputs = []Output{
		{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{1, 1}},
		{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{1, 1}},
	}
	vs := []Variant{{
		ID: "base", Rate: Rational{1, 20}, Valid: true,
		Outputs: append([]Output(nil), ec.Recipe.Outputs...),
	}}
	if err := Validate(ec, vs); err != nil {
		t.Fatalf("a variant echoing the recipe's own duplicate output ref must be accepted: %v", err)
	}
}
