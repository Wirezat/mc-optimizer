package solver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

// MaxVariantIterations bounds the fixed-point iteration that runs when a chosen
// variant changes a recipe's output amounts.
const MaxVariantIterations = 4

// VariantSource yields the evaluated variants for a (machine, recipe) pair
// under the mod config that applies to the machine's ecosystem.
type VariantSource interface {
	Variants(ctx context.Context, machine *MachineSpec, recipe *RecipeRow, config json.RawMessage) ([]plugins.Variant, error)
}

// DefaultVariant is the behavior for machines whose mod ships no plugin:
// nominal recipe duration, no operating costs, catalog outputs.
func DefaultVariant(recipe *RecipeRow) plugins.Variant {
	ticks := int64(recipe.DurationTicks)
	if ticks < 1 {
		ticks = 1
	}
	return plugins.Variant{
		ID:    "default",
		Rate:  plugins.Rational{Num: 1, Den: ticks},
		Valid: true,
	}
}

// PickVariant selects the variant from vs that reaches recipeRate with the
// fewest machines; ties are broken by the fewer installed items. Returns the
// variant, the machine count, and whether any valid variant was present at all.
func PickVariant(vs []plugins.Variant, recipeRate Rational) (plugins.Variant, int64, bool) {
	var best plugins.Variant
	var bestCount int64
	bestItems := 0
	found := false

	for _, v := range vs {
		if !v.Valid || v.Rate.Num <= 0 || v.Rate.Den <= 0 {
			continue
		}
		// Machines = ceil(required rate / rate of a single machine).
		count := max(recipeRate.Div(NewRational(v.Rate.Num, v.Rate.Den)).CeilInt(), 1)
		items := totalItems(v)
		if !found || count < bestCount || (count == bestCount && items < bestItems) {
			best, bestCount, bestItems, found = v, count, items, true
		}
	}
	return best, bestCount, found
}

func totalItems(v plugins.Variant) int {
	n := 0
	for _, it := range v.Items {
		n += it.Count
	}
	return n
}

// variantsFor returns the variants for one machine group. Without a configured
// source the host default applies.
func (s *Solver) variantsFor(ctx context.Context, machine *MachineSpec, recipe *RecipeRow, req SolveRequest) ([]plugins.Variant, error) {
	if s.VariantSource == nil {
		return []plugins.Variant{DefaultVariant(recipe)}, nil
	}
	vs, err := s.VariantSource.Variants(ctx, machine, recipe, req.ModConfigs[PluginMod(machine)])
	if err != nil {
		return nil, err
	}
	if len(vs) == 0 {
		return []plugins.Variant{DefaultVariant(recipe)}, nil
	}
	return vs, nil
}

// PluginMod names the mod whose plugin evaluates this machine: its ecosystem,
// or the machine's own mod when no ecosystem is set.
func PluginMod(machine *MachineSpec) string {
	if machine.Ecosystem != "" {
		return machine.Ecosystem
	}
	return machine.ModID
}

// baseVariant returns the variant without installed items, falling back to the
// first usable entry. Needed when no variant is runnable and the group still
// has to be costed.
func baseVariant(vs []plugins.Variant, recipe *RecipeRow) plugins.Variant {
	for _, v := range vs {
		if len(v.Items) == 0 && v.Rate.Num > 0 && v.Rate.Den > 0 {
			return v
		}
	}
	if len(vs) > 0 && vs[0].Rate.Num > 0 && vs[0].Rate.Den > 0 {
		return vs[0]
	}
	return DefaultVariant(recipe)
}

// variantChoice is the outcome of variant selection for a single machine group.
type variantChoice struct {
	variant     plugins.Variant
	count       int64
	exact       Rational
	utilization Rational
	runnable    bool
}

// chooseVariant picks the variant for one group and derives its machine counts
// from recipeRate (recipes per tick), reporting the failure as an error instead
// of letting the arithmetic panic. pinnedID, when it names a valid variant in
// vs, wins over the automatic pick; an absent or invalid pin is ignored.
func chooseVariant(vs []plugins.Variant, recipeRate Rational, recipe *RecipeRow, pinnedID string) (c variantChoice, err error) {
	defer guardRateArithmetic(&err)
	if pinned, ok := pinnedVariant(vs, pinnedID); ok {
		c.variant = pinned
		c.count = max(recipeRate.Div(NewRational(c.variant.Rate.Num, c.variant.Rate.Den)).CeilInt(), 1)
		c.runnable = true
	} else {
		c.variant, c.count, c.runnable = PickVariant(vs, recipeRate)
		if !c.runnable {
			c.variant = baseVariant(vs, recipe)
			c.count = max(recipeRate.Div(NewRational(c.variant.Rate.Num, c.variant.Rate.Den)).CeilInt(), 1)
		}
	}
	c.exact = recipeRate.Div(NewRational(c.variant.Rate.Num, c.variant.Rate.Den))
	c.utilization = c.exact.Div(NewRational(c.count, 1))
	return c, nil
}

// pinnedVariant returns the variant with the given id, provided it exists and
// is runnable. An empty id pins nothing.
func pinnedVariant(vs []plugins.Variant, id string) (plugins.Variant, bool) {
	if id == "" {
		return plugins.Variant{}, false
	}
	for _, v := range vs {
		if v.ID == id && v.Valid && v.Rate.Num > 0 && v.Rate.Den > 0 {
			return v, true
		}
	}
	return plugins.Variant{}, false
}

// variantOptions lists the runnable variants of a group for a client picker.
func variantOptions(vs []plugins.Variant) []VariantOption {
	out := make([]VariantOption, 0, len(vs))
	for _, v := range vs {
		if !v.Valid || v.Rate.Num <= 0 || v.Rate.Den <= 0 {
			continue
		}
		out = append(out, VariantOption{ID: v.ID, Label: v.Label})
	}
	return out
}

// EffectiveOutputs maps a variant's output overrides to the net amount per
// craft (amount times probability), keyed by output ref. Nil when the variant
// leaves the catalog amounts alone.
func EffectiveOutputs(v plugins.Variant) map[string]plugins.Rational {
	if len(v.Outputs) == 0 {
		return nil
	}
	out := make(map[string]plugins.Rational, len(v.Outputs))
	for _, o := range v.Outputs {
		if o.Amount.Den <= 0 {
			continue
		}
		// A wholly missing probability field arrives as 0/0; treat it as 1/1,
		// the same default plugins.Validate applies.
		prob := Rational{Num: 1, Den: 1}
		if o.Probability.Den > 0 {
			prob = NewRational(o.Probability.Num, o.Probability.Den)
		}
		net := NewRational(o.Amount.Num, o.Amount.Den).Mul(prob)
		out[o.Ref] = plugins.Rational{Num: net.Num, Den: net.Den}
	}
	return out
}

// outputBaseline holds one recipe node's catalog output amounts so a later round
// can restore them once the winning variant stops overriding them.
type outputBaseline struct {
	primary Rational
	edges   []Rational
}

// captureOutputBaseline snapshots the catalog output amounts of every recipe
// node, keyed by item key.
func captureOutputBaseline(g *RecipeGraph) map[string]outputBaseline {
	base := make(map[string]outputBaseline, len(g.Nodes))
	for key, node := range g.Nodes {
		if node.RecipeID == "" {
			continue
		}
		edges := make([]Rational, len(node.Outputs))
		for i, e := range node.Outputs {
			edges[i] = e.Amount
		}
		base[key] = outputBaseline{primary: node.OutputAmount, edges: edges}
	}
	return base
}

// syncVariantOutputs reconciles the graph's output amounts with the variants
// the groups chose and reports whether the two differed; with apply set, the
// difference is written into the graph. Groups are matched to nodes by
// RecipeOptionKey, never by RecipeID alone.
func syncVariantOutputs(g *RecipeGraph, base map[string]outputBaseline, groups []MachineGroupDraft, apply bool) bool {
	chosen := make(map[string]plugins.Variant, len(groups))
	for _, gr := range groups {
		chosen[RecipeOptionKey(gr.RecipeID, gr.MachineMod, gr.MachineID)] = gr.Variant
	}

	changed := false
	for key, node := range g.Nodes {
		b, ok := base[key]
		if !ok {
			continue
		}
		overrides := EffectiveOutputs(chosen[node.RateKey()])
		want := b.primary
		if o, ok := overrides[key]; ok {
			want = Rational{Num: o.Num, Den: o.Den}
		}
		if !want.Eq(node.OutputAmount) {
			changed = true
			if apply {
				node.OutputAmount = want
			}
		}
		for i := range node.Outputs {
			edgeWant := b.edges[i]
			if o, ok := overrides[node.Outputs[i].Item.Key()]; ok {
				edgeWant = Rational{Num: o.Num, Den: o.Den}
			}
			// Edge.Probability keeps its catalog value on purpose: EffectiveOutputs
			// folds probability into the amount exactly as graph.go stores it, and
			// no consumer reads an output edge's Probability separately.
			if !edgeWant.Eq(node.Outputs[i].Amount) {
				changed = true
				if apply {
					node.Outputs[i].Amount = edgeWant
				}
			}
		}
	}
	return changed
}

// repickVariants repeats the automatic variant choice for every unpinned group
// after AUTO scaling. The pick in CalculateMachineGroups saw the unscaled
// request rate, where nearly every group is a fraction of one machine and no
// upgrade can pay off. Each group keeps its recipe rate, so only its count and
// utilization move; variants that override outputs are left out because the
// rates were solved on the catalog amounts.
func repickVariants(groups []MachineGroupDraft, req SolveRequest) []MachineGroupDraft {
	for i := range groups {
		g := &groups[i]
		if len(g.variants) < 2 || len(g.Variant.Outputs) > 0 {
			continue
		}
		if _, pinned := pinnedVariant(g.variants, req.VariantPins[RecipeOptionKey(g.RecipeID, g.MachineMod, g.MachineID)]); pinned {
			continue
		}
		candidates := make([]plugins.Variant, 0, len(g.variants))
		for _, v := range g.variants {
			if len(v.Outputs) == 0 {
				candidates = append(candidates, v)
			}
		}
		recipeRate := g.ExactCount.Mul(NewRational(g.Variant.Rate.Num, g.Variant.Rate.Den))
		best, count, ok := PickVariant(candidates, recipeRate)
		if !ok || best.ID == g.VariantID {
			continue
		}
		g.Variant, g.VariantID, g.Label, g.Costs = best, best.ID, best.Label, best.Costs
		g.ExactCount = recipeRate.Div(NewRational(best.Rate.Num, best.Rate.Den))
		g.Count = count
		g.Utilization = g.ExactCount.Div(NewRational(count, 1))
	}
	return groups
}

// RecountForVariant recomputes a machine group's counts when it switches from
// variant from to variant to, reconstructing the recipe rate from the group's
// fractional count under the old variant. Returns the new machine count and
// the new fractional count.
func RecountForVariant(exact Rational, from, to plugins.Variant) (count int64, newExact Rational, err error) {
	defer guardRateArithmetic(&err)
	if from.Rate.Num <= 0 || from.Rate.Den <= 0 {
		return 0, Rational{}, fmt.Errorf("solver: variant %q has no usable rate", from.ID)
	}
	if to.Rate.Num <= 0 || to.Rate.Den <= 0 {
		return 0, Rational{}, fmt.Errorf("solver: variant %q has no usable rate", to.ID)
	}
	recipeRate := exact.Mul(NewRational(from.Rate.Num, from.Rate.Den))
	newExact = recipeRate.Div(NewRational(to.Rate.Num, to.Rate.Den))
	return max(newExact.CeilInt(), 1), newExact, nil
}
