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

// On a blasting recipe every cell carries the red augment, so there is none without items.
func TestValidateAllowsZeroBaseVariants(t *testing.T) {
	prog := loadFixture(t, "no_base.js")
	got, err := prog.Evaluate(context.Background(), sampleContext())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(got) != 1 || len(got[0].Items) == 0 {
		t.Fatalf("got %+v, want the one variant that installs an item", got)
	}
}

// Two base variants stay an error.
func TestValidateRejectsTwoBaseVariants(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{
		{ID: "base", Rate: Rational{1, 20}, Outputs: ec.Recipe.Outputs, Valid: true},
		{ID: "also_base", Rate: Rational{1, 30}, Outputs: ec.Recipe.Outputs, Valid: true},
	}
	err := Validate(ec, vs)
	if err == nil {
		t.Fatal("want error for two base variants, got nil")
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

// TestValidateRejectsZeroDenominator: Rational{1, 0} is a zero denominator, not a missing field.
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

// TestValidateRejectsMissingField: Rational{0, 0} is reported as missing, not as a zero denominator.
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

// TestValidateRejectsEmptyResult pins down the len(vs) == 0 check specifically, not just "some error".
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

// TestValidateDefaultsMissingProbabilityWithoutMutatingSharedSlice: defaulting writes into a copy of Outputs.
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

// TestValidateRejectsNegativeDenominator asserts that a negative denominator such as -20 is rejected.
func TestValidateRejectsNegativeDenominator(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: 1, Den: -20}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a negative denominator, got nil")
	}
}

// TestValidateRejectsOversizedMagnitude asserts that a numerator one past maxPluginMagnitude is rejected.
func TestValidateRejectsOversizedMagnitude(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: maxPluginMagnitude + 1, Den: 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a numerator beyond the plugin magnitude bound, got nil")
	}
}

// TestValidateRejectsOversizedDenominator is the Den-side counterpart of TestValidateRejectsOversizedMagnitude.
func TestValidateRejectsOversizedDenominator(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: 1, Den: maxPluginMagnitude + 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a denominator beyond the plugin magnitude bound, got nil")
	}
}

// TestValidateRejectsMinInt64 asserts that math.MinInt64 is rejected.
func TestValidateRejectsMinInt64(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: math.MinInt64, Den: 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for math.MinInt64, got nil")
	}
}

// TestValidateRejectsZeroRate asserts that rate == 0 is rejected.
func TestValidateRejectsZeroRate(t *testing.T) {
	ec := sampleContext()
	vs := []Variant{{ID: "base", Rate: Rational{Num: 0, Den: 1}, Outputs: ec.Recipe.Outputs, Valid: true}}
	if err := Validate(ec, vs); err == nil {
		t.Fatal("want error for a zero rate, got nil")
	}
}

// TestValidateAcceptsZeroAmountAndProbability: zero is rejected only for rate.
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

// TestValidateRejectsProbabilityAboveOne asserts that a probability above 1 is rejected.
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

// TestValidateRejectsEmptyID and TestValidateRejectsDuplicateID: each id check on its own.
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

// TestValidateRejectsEmptyCostResource asserts that a cost with an empty resource name is rejected.
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

// Installed item counts must be positive and within the plugin magnitude bound.
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

// Each of checkRational's other call sites (cost amount, output amount, output probability) on its own.
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

// TestValidateRejectsUnknownOutputRef isolates the per-output membership check from the multiset count.
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

// TestValidateRejectsOutputMultisetMismatch: outputs are compared as a multiset per ref.
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

// TestValidateAcceptsDuplicateRecipeOutputsEchoedBack: a repeated recipe output echoed back is accepted.
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
