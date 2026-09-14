package solver

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/plugins"
)

// CalculateMachineGroups converts recipe rates into machine group drafts, one per
// (recipe, machine) option. Returns the drafts and any warnings collected on the
// way; caller must authorize ownership if needed.
//
// A plugin that fails degrades its own mod to the host default and adds a
// warning (spec section 10); only genuine host failures return an error.
func (s *Solver) CalculateMachineGroups(ctx context.Context, g *RecipeGraph, rv RateVector, req SolveRequest) ([]MachineGroupDraft, []Warning, error) {
	var groups []MachineGroupDraft
	var warnings []Warning
	// Warns once per mod, not once per group.
	failedMods := make(map[string]bool)

	// build reverse map: RateKey(recipe, machine) → produced item
	rateKeyToItem := make(map[string]ItemRef, len(g.Nodes))
	for _, node := range g.Nodes {
		if node.RecipeID != "" {
			rateKeyToItem[node.RateKey()] = node.Item
		}
	}

	for rateKey, recipeRate := range rv.RecipeRates {
		if recipeRate.IsZero() {
			continue
		}
		recipeID, machineMod, machineID, ok := ParseRecipeOptionKey(rateKey)
		if !ok {
			return nil, nil, fmt.Errorf("solver: malformed rate key %q", rateKey)
		}
		recipe, err := s.DB.GetRecipe(ctx, recipeID)
		if err != nil {
			return nil, nil, fmt.Errorf("solver: get recipe %s: %w", recipeID, err)
		}
		if recipe == nil {
			return nil, nil, fmt.Errorf("solver: recipe %s not found", recipeID)
		}
		// Use the CHOSEN machine (may be a variant reached via machine_interfaces), not
		// recipe.MachineMod/MachineID — that always points at the recipe's own canonical
		// machine, which would silently discard the user's choice.
		machine, err := s.DB.GetMachineType(ctx, machineMod, machineID)
		if err != nil {
			return nil, nil, fmt.Errorf("solver: get machine %s:%s: %w", machineMod, machineID, err)
		}
		if machine == nil {
			return nil, nil, fmt.Errorf("solver: machine %s:%s not found", machineMod, machineID)
		}

		vs, err := s.variantsFor(ctx, machine, recipe, req)
		if err != nil {
			mod := PluginMod(machine)
			if !failedMods[mod] {
				failedMods[mod] = true
				// The raw error can be a pgx error carrying host, user, and
				// database name; /api/demo/solve is unauthenticated, so the
				// client gets only the mod id and the full text goes to the
				// server log instead.
				GoLog.Warnf("solver: plugin failed for mod %s: %v", mod, err)
				warnings = append(warnings, Warning{
					Code:   "plugin_failed",
					Params: map[string]string{"mod": mod},
				})
			}
			vs = []plugins.Variant{DefaultVariant(recipe)}
		}

		choice, err := chooseVariant(vs, recipeRate, recipe, req.VariantPins[rateKey])
		if err != nil {
			return nil, nil, fmt.Errorf("solver: recipe %s on %s:%s: %w", recipeID, machineMod, machineID, err)
		}
		if !choice.runnable {
			// No variant can run this recipe: cost the base case and warn instead
			// of refusing the whole solution.
			warnings = append(warnings, Warning{
				Code:   "recipe_not_runnable",
				Params: map[string]string{"recipe": recipeID, "machine": machineMod + ":" + machineID},
			})
		}

		groups = append(groups, MachineGroupDraft{
			MachineMod:     machine.ModID,
			MachineID:      machine.MachineID,
			RecipeID:       recipeID,
			RecipeOutput:   rateKeyToItem[rateKey],
			Status:         StatusDraft,
			VariantID:      choice.variant.ID,
			Label:          choice.variant.Label,
			PluginMod:      PluginMod(machine),
			Costs:          choice.variant.Costs,
			Variant:        choice.variant,
			VariantOptions: variantOptions(vs),
			variants:       vs,
			ExactCount:     choice.exact,
			Count:          choice.count,
			Utilization:    choice.utilization,
		})
	}

	return groups, warnings, nil
}

// ScaleToInteger scales machine counts so that non-partial machines run at exactly 100% utilisation.
// Partial machines (listed in allowPartial) use ceil(ExactCount), supplying at least as much as
// needed while leaving some idle capacity — all non-partial machines still run at 100%.
// If the LCM overflows or exceeds maxScale, falls back gracefully to TARGET-mode ceil-counts.
// Returns scaled groups, actual rate, scale factor k, and warnings.
// k==1 means no scaling was applied (either no groups or scale exceeded maxScale).
func (s *Solver) ScaleToInteger(groups []MachineGroupDraft, rv RateVector, rootItem ItemRef, maxScale int64, allowPartial map[string]bool) ([]MachineGroupDraft, Rational, int64, []Warning) {
	var warnings []Warning

	if len(groups) == 0 {
		return groups, NewRational(0, 1), 1, warnings
	}

	hasPartial := len(allowPartial) > 0

	// LCM of non-partial machine denominators so their ExactCounts become integers (100% util).
	// If all machines are partial, fall back to using all.
	fractions := make([]Rational, 0, len(groups))
	for _, g := range groups {
		if !hasPartial || !allowPartial[g.RecipeID] {
			fractions = append(fractions, g.ExactCount)
		}
	}
	if len(fractions) == 0 {
		// All machines are partial — skip LCM scaling, just ceil each independently.
		result := make([]MachineGroupDraft, len(groups))
		copy(result, groups)
		for i := range result {
			result[i].Count = max(result[i].ExactCount.CeilInt(), 1)
			result[i].Utilization = result[i].ExactCount.Div(NewRational(result[i].Count, 1))
		}
		return result, rv.ItemRates[rootItem.Key()], 1, warnings
	}

	k := safeLCMOfFractions(fractions, maxScale)
	if k == 0 || k > maxScale {
		// No integer scale ≤ maxScale makes every machine exactly 100% (the recipes' batch
		// sizes are too mismatched). Instead of leaving everything at the tiny base rate,
		// scale so the busiest machine runs at exactly 100% (others as high as possible) —
		// the smallest factory that fully utilises its bottleneck.
		maxExact := fractions[0]
		maxV := float64(fractions[0].Num) / float64(fractions[0].Den)
		for _, f := range fractions[1:] {
			if v := float64(f.Num) / float64(f.Den); v > maxV {
				maxV, maxExact = v, f
			}
		}
		result := make([]MachineGroupDraft, len(groups))
		copy(result, groups)
		actualRate := rv.ItemRates[rootItem.Key()]
		if maxExact.Num > 0 {
			scale := NewRational(maxExact.Den, maxExact.Num) // 1 / maxExact → busiest becomes 1.0
			for i := range result {
				result[i].ExactCount = result[i].ExactCount.Mul(scale)
				result[i].Count = max(result[i].ExactCount.CeilInt(), 1)
				result[i].Utilization = result[i].ExactCount.Div(NewRational(result[i].Count, 1))
			}
			actualRate = actualRate.Mul(scale)
			// Scale the rate vector in place so the IO profile matches the scaled counts.
			// (Returning k=1 below stops Solve from scaling rv again.)
			for key, rate := range rv.ItemRates {
				rv.ItemRates[key] = rate.Mul(scale)
			}
		}
		warnings = append(warnings, Warning{
			Code:   "auto_scale_bottleneck",
			Params: map[string]string{"max_scale": strconv.FormatInt(maxScale, 10)},
		})
		return result, actualRate, 1, warnings
	}

	result := make([]MachineGroupDraft, len(groups))
	copy(result, groups)
	kRat := NewRational(k, 1)
	for i := range result {
		result[i].ExactCount = result[i].ExactCount.Mul(kRat)
	}
	actualRate := rv.ItemRates[rootItem.Key()].Mul(kRat)

	// GCD-reduce using non-partial counts (integers after LCM scaling) to find minimal solution.
	var g int64
	for _, gr := range result {
		if !hasPartial || !allowPartial[gr.RecipeID] {
			c := gr.ExactCount.CeilInt()
			if g == 0 {
				g = c
			} else {
				g = gcd(g, c)
			}
			if g == 1 {
				break
			}
		}
	}
	if g == 0 {
		// All machines are partial; GCD over all ceil counts.
		for _, gr := range result {
			c := gr.ExactCount.CeilInt()
			if g == 0 {
				g = c
			} else {
				g = gcd(g, c)
			}
		}
	}
	if g < 1 {
		g = 1
	}
	if g > 1 {
		gRat := NewRational(g, 1)
		for i := range result {
			result[i].ExactCount = result[i].ExactCount.Div(gRat)
		}
		actualRate = actualRate.Div(gRat)
	}

	// Set counts: non-partial get ExactCount (exact integer → 100%), partial get ceil(ExactCount).
	for i := range result {
		result[i].Count = max(result[i].ExactCount.CeilInt(), 1)
		result[i].Utilization = result[i].ExactCount.Div(NewRational(result[i].Count, 1))
	}

	// Final GCD pass: ceil of partial ExactCounts may introduce a new common factor.
	g2 := result[0].Count
	for _, gr := range result[1:] {
		g2 = gcd(g2, gr.Count)
		if g2 == 1 {
			break
		}
	}
	if g2 > 1 {
		g2Rat := NewRational(g2, 1)
		for i := range result {
			result[i].Count /= g2
			result[i].ExactCount = result[i].ExactCount.Div(g2Rat)
			result[i].Utilization = result[i].ExactCount.Div(NewRational(result[i].Count, 1))
		}
		actualRate = actualRate.Div(g2Rat)
	}

	return result, actualRate, k, warnings
}

// safeLCMOfFractions computes the LCM of fraction denominators, returning 0 on int64 overflow.
func safeLCMOfFractions(fractions []Rational, _ int64) (k int64) {
	defer func() {
		if recover() != nil {
			k = 0
		}
	}()
	return lcmOfFractions(fractions)
}

// ComputeIOProfile aggregates inputs and outputs from the rate vector and recipe graph.
// Rates are converted from per-tick to per-timeUnit for display.
// Byproducts (secondary outputs of recipe nodes) are included, with their consumption by
// other recipe nodes subtracted so only the net external flow appears.
func ComputeIOProfile(rv RateVector, g *RecipeGraph, factory FactoryState, timeUnit string) IOProfile {
	// Step 1: gross byproduct production rates (secondary outputs only).
	byproductGross := make(map[string]Rational)
	for key, node := range g.Nodes {
		if node.RecipeID == "" || node.OutputAmount.IsZero() || len(node.Outputs) == 0 {
			continue
		}
		rawRate := rv.ItemRates[key]
		if rawRate.IsZero() {
			continue
		}
		recipeRate := rawRate.Div(node.OutputAmount)
		for _, edge := range node.Outputs {
			bKey := edge.Item.Key()
			if bKey == key {
				continue
			}
			byproductGross[bKey] = byproductGross[bKey].Add(recipeRate.Mul(edge.Amount))
		}
	}

	var inputs, outputs []IOEntry

	// Step 2: root output + external inputs (raw materials / stop points).
	// For inputs consumed partly by byproducts, show only the net external demand.
	for key, node := range g.Nodes {
		rawRate := rv.ItemRates[key]
		if key == g.Root.Key() {
			if !rawRate.IsZero() {
				outputs = append(outputs, IOEntry{
					Item:     node.Item,
					Rate:     ConvertFromPerTick(rawRate, timeUnit),
					TimeUnit: timeUnit,
				})
			}
			continue
		}
		if node.IsRawMaterial || node.IsStopPoint || node.IsFactoryProvided {
			demand := rawRate
			if bpSupply, ok := byproductGross[key]; ok {
				demand = demand.Sub(bpSupply)
				if demand.Num < 0 {
					demand = Rational{}
				}
			}
			if demand.IsZero() {
				continue
			}
			inputs = append(inputs, IOEntry{
				Item:              node.Item,
				Rate:              ConvertFromPerTick(demand, timeUnit),
				TimeUnit:          timeUnit,
				IsStopPoint:       node.IsStopPoint,
				IsFactoryProvided: node.IsFactoryProvided,
			})
		}
	}

	// Step 3: net byproduct outputs (produced more than internally consumed).
	for bKey, produced := range byproductGross {
		consumed := rv.ItemRates[bKey]
		net := produced.Sub(consumed)
		if net.IsZero() || net.Num <= 0 {
			continue
		}
		var ref ItemRef
		if node, ok := g.Nodes[bKey]; ok {
			ref = node.Item
		} else {
			ref = itemRefFromKey(bKey)
		}
		outputs = append(outputs, IOEntry{
			Item:     ref,
			Rate:     ConvertFromPerTick(net, timeUnit),
			TimeUnit: timeUnit,
		})
	}

	return IOProfile{Inputs: inputs, Outputs: outputs}
}

// itemRefFromKey reconstructs an ItemRef from a graph key string.
// Fluid keys: "fluid:modID:itemID". Item keys: "modID:itemID" (no prefix).
func itemRefFromKey(key string) ItemRef {
	if len(key) > 6 && key[:6] == "fluid:" {
		rest := key[6:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == ':' {
				return ItemRef{ModID: rest[:i], ItemID: rest[i+1:], IsFluid: true}
			}
		}
	}
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return ItemRef{ModID: key[:i], ItemID: key[i+1:]}
		}
	}
	return ItemRef{ItemID: key}
}

// lcmOfFractions returns the least common multiple of the denominators of the given fractions.
func lcmOfFractions(fs []Rational) int64 {
	dens := make([]int64, len(fs))
	for i, f := range fs {
		dens[i] = f.Den
	}
	return LCM(dens)
}
