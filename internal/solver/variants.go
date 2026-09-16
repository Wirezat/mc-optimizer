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

// cell is one candidate: a machine, its recipe and one operating variant.
type cell struct {
	machine *MachineSpec
	recipe  *RecipeRow
	variant plugins.Variant

	count       int64
	exact       Rational
	utilization Rational
	byproduct   Rational
}

// ladderCtx carries the chain-wide figures stages 4 and 6 read; nil skips both.
type ladderCtx struct {
	affinity map[string]int // machine mod → groups won in pass 1
	yields   yieldIndex
}

// yieldIndex is an item per tick per machine at the node producing it.
type yieldIndex map[string]Rational

func indexYields(g *RecipeGraph, groups []MachineGroupDraft) yieldIndex {
	out := yieldIndex{}
	for _, gr := range groups {
		node := g.Nodes[gr.RecipeOutput.Key()]
		if node == nil || node.RecipeID == "" {
			continue
		}
		perMachine := node.OutputAmount.Mul(NewRational(gr.Variant.Rate.Num, gr.Variant.Rate.Den))
		if !perMachine.IsZero() {
			out[node.Item.Key()] = perMachine
		}
	}
	return out
}

// byproductValue is the machines a cell saves elsewhere by raising an output the
// chain also produces. One step only and never rounded up, so it under-counts.
func byproductValue(c cell, demand Rational, yi yieldIndex) Rational {
	saved := NewRational(0, 1)
	if len(yi) == 0 || c.recipe == nil {
		return saved
	}
	base := catalogOutputs(c.recipe)
	for ref, amount := range EffectiveOutputs(c.variant) {
		b, ok := base[ref]
		if !ok {
			continue
		}
		per, ok := yi[ref]
		if !ok || per.IsZero() {
			continue
		}
		delta := NewRational(amount.Num, amount.Den).Sub(b)
		if !delta.IsPositive() {
			continue
		}
		saved = saved.Add(delta.Mul(demand).Div(per))
	}
	return saved
}

func catalogOutputs(r *RecipeRow) map[string]Rational {
	out := make(map[string]Rational, len(r.ItemOutputs)+len(r.FluidOutputs))
	for _, o := range r.ItemOutputs {
		if o.ItemModID == nil || o.ItemID == nil {
			continue
		}
		ref := ItemRef{ModID: *o.ItemModID, ItemID: *o.ItemID}
		out[ref.Key()] = NewRational(o.AmountNum, o.AmountDen).Mul(NewRational(o.ProbabilityNum, o.ProbabilityDen))
	}
	for _, f := range r.FluidOutputs {
		ref := ItemRef{ModID: f.FluidModID, ItemID: f.FluidID, IsFluid: true}
		out[ref.Key()] = NewRational(f.AmountMB, 1).Mul(NewRational(f.ProbabilityNum, f.ProbabilityDen))
	}
	return out
}

// pickCell picks one cell of a node; false when none can run the recipe.
//
//	1 valid                        2 min ceil(demand/rate)
//	3 max utilization              4 max byproduct value
//	5 min consumption per resource 6 max mod affinity
//	7 min rank                     8 (mod, machine, variant id)
func pickCell(cells []cell, demand Rational, lc *ladderCtx) (cell, bool) {
	rem := make([]cell, 0, len(cells))
	for _, c := range cells {
		if !c.variant.Valid || c.variant.Rate.Num <= 0 || c.variant.Rate.Den <= 0 {
			continue
		}
		rem = append(rem, measure(c, demand))
	}
	if len(rem) == 0 {
		return cell{}, false
	}
	rem = keepBest(rem, func(a, b cell) int { return cmpInt64(a.count, b.count) })
	rem = keepBest(rem, func(a, b cell) int { return -a.utilization.Cmp(b.utilization) })
	if lc != nil && len(lc.yields) > 0 {
		for i := range rem {
			rem[i].byproduct = byproductValue(rem[i], demand, lc.yields)
		}
		// Whole machines only: a tenth of one must not outweigh the energy the
		// cell draws for it, which is stage 5's call.
		rem = keepBest(rem, func(a, b cell) int {
			return -cmpInt64(a.byproduct.FloorInt(), b.byproduct.FloorInt())
		})
	}
	rem = keepCheapestPerResource(rem)
	if lc != nil {
		rem = keepBest(rem, func(a, b cell) int {
			return -cmpInt64(int64(lc.affinity[a.machine.ModID]), int64(lc.affinity[b.machine.ModID]))
		})
	}
	rem = keepMinRank(rem)
	return leastCell(rem), true
}

func measure(c cell, demand Rational) cell {
	c.exact = demand.Div(NewRational(c.variant.Rate.Num, c.variant.Rate.Den))
	c.count = max(c.exact.CeilInt(), 1)
	c.utilization = c.exact.Div(NewRational(c.count, 1))
	return c
}

func keepBest(cells []cell, cmp func(a, b cell) int) []cell {
	out := make([]cell, 0, len(cells))
	out = append(out, cells[0])
	for _, c := range cells[1:] {
		switch cmp(c, out[0]) {
		case -1:
			out = append(out[:0], c)
		case 0:
			out = append(out, c)
		}
	}
	return out
}

// keepCheapestPerResource drops a cell when another with the same resource set
// is nowhere dearer. rf, eu and coal have no exchange rate.
func keepCheapestPerResource(cells []cell) []cell {
	costs := make([]map[string]Rational, len(cells))
	for i, c := range cells {
		costs[i] = costOf(c.variant)
	}
	out := make([]cell, 0, len(cells))
	for i := range cells {
		dominated := false
		for j := range cells {
			if i != j && cheaper(costs[j], costs[i]) {
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, cells[i])
		}
	}
	if len(out) == 0 {
		return cells
	}
	return out
}

func costOf(v plugins.Variant) map[string]Rational {
	out := make(map[string]Rational, len(v.Costs))
	for _, c := range v.Costs {
		if c.Amount.Den <= 0 {
			continue
		}
		out[c.Resource] = out[c.Resource].Add(NewRational(c.Amount.Num, c.Amount.Den))
	}
	return out
}

func cheaper(a, b map[string]Rational) bool {
	if len(a) != len(b) {
		return false
	}
	less := false
	for res, av := range a {
		bv, ok := b[res]
		if !ok {
			return false
		}
		switch av.Cmp(bv) {
		case 1:
			return false
		case -1:
			less = true
		}
	}
	return less
}

// keepMinRank compares rank only while all cells share one plugin.
func keepMinRank(cells []cell) []cell {
	mod := PluginMod(cells[0].machine)
	for _, c := range cells[1:] {
		if PluginMod(c.machine) != mod {
			return cells
		}
	}
	return keepBest(cells, func(a, b cell) int {
		return cmpInt64(int64(a.variant.Rank), int64(b.variant.Rank))
	})
}

func leastCell(cells []cell) cell {
	best := cells[0]
	for _, c := range cells[1:] {
		if cellIdentityLess(c, best) {
			best = c
		}
	}
	return best
}

func cellIdentityLess(a, b cell) bool {
	if a.machine.ModID != b.machine.ModID {
		return a.machine.ModID < b.machine.ModID
	}
	if a.machine.MachineID != b.machine.MachineID {
		return a.machine.MachineID < b.machine.MachineID
	}
	if a.variant.ID != b.variant.ID {
		return a.variant.ID < b.variant.ID
	}
	return a.recipe.ID < b.recipe.ID
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
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

// chooseCell runs the ladder over a node's matrix at recipeRate (recipes per
// tick). A pin narrows the matrix first; !runnable costs the node's own machine.
func chooseCell(m nodeMatrix, recipeRate Rational, lc *ladderCtx, pinnedID string) (c cell, runnable bool, err error) {
	defer guardRateArithmetic(&err)
	cells := m.cells
	if pinned := pinnedCells(cells, pinnedID); len(pinned) > 0 {
		cells = pinned
	}
	if best, ok := pickCell(cells, recipeRate, lc); ok {
		return best, true, nil
	}
	fallback := cell{machine: m.machine, recipe: m.recipe, variant: baseVariant(m.variants, m.recipe)}
	return measure(fallback, recipeRate), false, nil
}

func pinnedCells(cells []cell, id string) []cell {
	if id == "" {
		return nil
	}
	out := make([]cell, 0, 1)
	for _, c := range cells {
		if c.variant.ID == id && c.variant.Valid && c.variant.Rate.Num > 0 && c.variant.Rate.Den > 0 {
			out = append(out, c)
		}
	}
	return out
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

// variantOptionsFor lists the chosen machine's runnable variants.
func variantOptionsFor(cells []cell, machine *MachineSpec) []VariantOption {
	out := make([]VariantOption, 0, len(cells))
	for _, c := range cells {
		if c.machine.ModID != machine.ModID || c.machine.MachineID != machine.MachineID {
			continue
		}
		if !c.variant.Valid || c.variant.Rate.Num <= 0 || c.variant.Rate.Den <= 0 {
			continue
		}
		out = append(out, VariantOption{ID: c.variant.ID, Label: c.variant.Label})
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
// difference is written into the graph. Groups match nodes by their rate key -
// the ladder may have put another machine or recipe in the group.
func syncVariantOutputs(g *RecipeGraph, base map[string]outputBaseline, groups []MachineGroupDraft, apply bool) bool {
	chosen := make(map[string]plugins.Variant, len(groups))
	for _, gr := range groups {
		chosen[gr.RateKey] = gr.Variant
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

// repickVariants runs the ladder again at the scaled rate; the first pick saw
// fractions of a machine. Cells overriding outputs are left out - the rates
// were solved on catalog amounts.
func repickVariants(groups []MachineGroupDraft, req SolveRequest, lc *ladderCtx) []MachineGroupDraft {
	for i := range groups {
		g := &groups[i]
		if len(g.cells) < 2 || len(g.Variant.Outputs) > 0 {
			continue
		}
		if len(pinnedCells(g.cells, req.VariantPins[g.RateKey])) > 0 {
			continue
		}
		candidates := make([]cell, 0, len(g.cells))
		for _, c := range g.cells {
			if len(c.variant.Outputs) == 0 {
				candidates = append(candidates, c)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		recipeRate := g.ExactCount.Mul(NewRational(g.Variant.Rate.Num, g.Variant.Rate.Den))
		best, ok := pickCell(candidates, recipeRate, lc)
		if !ok || (best.variant.ID == g.VariantID && best.machine.ModID == g.MachineMod && best.machine.MachineID == g.MachineID) {
			continue
		}
		g.applyCell(best)
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
