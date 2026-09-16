package plugins

import (
	"fmt"
	"math"
	"sort"
)

// maxPluginMagnitude bounds every Num, Den, and Item.Count value a plugin may
// return. See handoff section 11 for the choice of math.MaxInt32 and for why it
// does not by itself make chained arithmetic overflow-safe.
const maxPluginMagnitude = math.MaxInt32

// Validate checks a variant list against the plugin contract: exactly one base
// variant (no installed items), an output set that either matches the recipe's
// exactly as a multiset or is left empty, well-formed fractions, a strictly
// positive rate, and output probabilities within [0, 1]. A missing output
// probability defaults to 1/1 in a copy of Outputs, never the caller's slice.
func Validate(ec EvalContext, vs []Variant) error {
	if len(vs) == 0 {
		return fmt.Errorf("plugins: evaluate returned no variants")
	}

	want := make(map[string]int, len(ec.Recipe.Outputs))
	for _, o := range ec.Recipe.Outputs {
		want[o.Ref]++
	}

	bases := 0
	seen := make(map[string]bool, len(vs))
	for i := range vs {
		v := &vs[i]
		if v.ID == "" {
			return fmt.Errorf("plugins: variant %d has an empty id", i)
		}
		if seen[v.ID] {
			return fmt.Errorf("plugins: duplicate variant id %q", v.ID)
		}
		seen[v.ID] = true

		if err := checkRational("rate", v.ID, v.Rate, false); err != nil {
			return err
		}
		for _, c := range v.Costs {
			if c.Resource == "" {
				return fmt.Errorf("plugins: variant %q has a cost with an empty resource", v.ID)
			}
			if err := checkRational("cost amount", v.ID, c.Amount, true); err != nil {
				return err
			}
		}

		if len(v.Items) == 0 {
			bases++
		}
		for _, it := range v.Items {
			if it.Count <= 0 {
				return fmt.Errorf("plugins: variant %q installs %q with count %d", v.ID, it.Ref, it.Count)
			}
			if it.Count > maxPluginMagnitude {
				return fmt.Errorf("plugins: variant %q installs %q with count %d beyond the plugin magnitude bound %d", v.ID, it.Ref, it.Count, maxPluginMagnitude)
			}
		}

		// An empty Outputs means "unchanged from the recipe catalog".
		if len(v.Outputs) > 0 {
			got := make(map[string]int, len(v.Outputs))
			outs := make([]Output, len(v.Outputs))
			copy(outs, v.Outputs)
			for j := range outs {
				o := &outs[j]
				if want[o.Ref] == 0 {
					return fmt.Errorf("plugins: variant %q changes the output set (%q is not a recipe output)", v.ID, o.Ref)
				}
				got[o.Ref]++
				if err := checkRational("output amount", v.ID, o.Amount, true); err != nil {
					return err
				}
				// A wholly missing probability field extracts as the zero Rational; it counts
				// as 1/1.
				if o.Probability.Den == 0 {
					o.Probability = Rational{Num: 1, Den: 1}
				}
				if err := checkRational("output probability", v.ID, o.Probability, true); err != nil {
					return err
				}
				if o.Probability.Num > o.Probability.Den {
					return fmt.Errorf("plugins: variant %q has output probability greater than 1 (%d/%d)", v.ID, o.Probability.Num, o.Probability.Den)
				}
			}
			// Refs are reported in sorted order for a stable message.
			refs := make([]string, 0, len(want))
			for ref := range want {
				refs = append(refs, ref)
			}
			sort.Strings(refs)
			for _, ref := range refs {
				n := want[ref]
				if got[ref] != n {
					unit := "times"
					if got[ref] == 1 {
						unit = "time"
					}
					return fmt.Errorf("plugins: variant %q changes the output set (%q appears %d %s, recipe has %d)", v.ID, ref, got[ref], unit, n)
				}
			}
			v.Outputs = outs
		}
	}

	// At most one: on a blasting recipe every cell carries the red augment, and
	// both baseVariant implementations fall back on their own.
	if bases > 1 {
		return fmt.Errorf("plugins: expected at most one base variant with no items, got %d", bases)
	}
	return nil
}

// checkRational rejects a fraction the solver cannot safely compute with: a
// non-positive denominator, a negative numerator, or either magnitude above
// maxPluginMagnitude.
func checkRational(field, variantID string, r Rational, allowZero bool) error {
	if r.Num == 0 && r.Den == 0 {
		return fmt.Errorf("plugins: variant %q is missing %s", variantID, field)
	}
	if r.Den == 0 {
		return fmt.Errorf("plugins: variant %q has %s with a zero denominator (%d/%d)", variantID, field, r.Num, r.Den)
	}
	if r.Den < 0 {
		return fmt.Errorf("plugins: variant %q has %s with a negative denominator (%d/%d)", variantID, field, r.Num, r.Den)
	}
	if r.Num < 0 {
		return fmt.Errorf("plugins: variant %q has negative %s (%d/%d)", variantID, field, r.Num, r.Den)
	}
	if !allowZero && r.Num == 0 {
		return fmt.Errorf("plugins: variant %q has a zero %s", variantID, field)
	}
	if r.Num > maxPluginMagnitude {
		return fmt.Errorf("plugins: variant %q has %s numerator %d beyond the plugin magnitude bound %d", variantID, field, r.Num, maxPluginMagnitude)
	}
	if r.Den > maxPluginMagnitude {
		return fmt.Errorf("plugins: variant %q has %s denominator %d beyond the plugin magnitude bound %d", variantID, field, r.Den, maxPluginMagnitude)
	}
	return nil
}
