package solver

import (
	"context"
	"fmt"
	"strconv"
)

// CalculateMachineGroups converts recipe rates into machine group drafts.
// Returns a slice of MachineGroupDraft; caller must authorize ownership if needed.
func (s *Solver) CalculateMachineGroups(ctx context.Context, g *RecipeGraph, rv RateVector) ([]MachineGroupDraft, error) {
	var groups []MachineGroupDraft

	// build reverse map: recipeID → produced item
	recipeToItem := make(map[string]ItemRef, len(g.Nodes))
	for _, node := range g.Nodes {
		if node.RecipeID != "" {
			recipeToItem[node.RecipeID] = node.Item
		}
	}

	for recipeID, recipeRate := range rv.RecipeRates {
		if recipeRate.IsZero() {
			continue
		}
		recipe, err := s.DB.GetRecipe(ctx, recipeID)
		if err != nil {
			return nil, fmt.Errorf("solver: get recipe %s: %w", recipeID, err)
		}
		machine, err := s.DB.GetMachineType(ctx, recipe.MachineMod, recipe.MachineID)
		if err != nil {
			return nil, fmt.Errorf("solver: get machine %s:%s: %w", recipe.MachineMod, recipe.MachineID, err)
		}

		group := MachineGroupDraft{
			MachineMod:   machine.ModID,
			MachineID:    machine.MachineID,
			RecipeID:     recipeID,
			RecipeOutput: recipeToItem[recipeID],
			Status:       StatusDraft,
		}
		// Base case (no upgrades): electric machines already overclock to their base
		// max EU/t, so effectiveTicks may be lower than the nominal recipe duration.
		groups = append(groups, applyUpgrade(group, recipe, machine, 0, 0, recipeRate))
	}

	return groups, nil
}

// EffectiveTicks returns the number of ticks one craft takes when the machine runs with
// n upgrade slots of the given per-slot EU bonus. For machines that are not upgradable
// EU machines it returns the nominal recipe duration (their craft speed is fixed).
// Exported for the PL-edit recompute path; the solver uses it internally too.
func EffectiveTicks(recipe *RecipeRow, machine *MachineSpec, euBonusPerSlot int64, n int) int64 {
	return effectiveTicks(recipe, machine, euBonusPerSlot, n)
}

func effectiveTicks(recipe *RecipeRow, machine *MachineSpec, euBonusPerSlot int64, n int) int64 {
	duration := int64(max(recipe.DurationTicks, 1))
	if machine == nil || !machine.Upgradable || machine.EnergyType != "eu" {
		return duration
	}
	total := recipe.TotalEU
	if total <= 0 {
		total = recipe.EUPerTick * duration
	}
	if total <= 0 {
		return duration
	}
	baseMax := machine.MaxEUPerTick
	if baseMax <= 0 {
		baseMax = defaultBaseMaxEU
	}
	effectiveEU := min(baseMax+int64(n)*euBonusPerSlot, total)
	if effectiveEU <= 0 {
		return duration
	}
	return ceilDiv(total, effectiveEU)
}

// upgradeSlotCap returns the maximum number of upgrade slots allowed for a machine.
func upgradeSlotCap(machine *MachineSpec) int {
	if machine != nil && machine.MaxSlots > 0 {
		return int(machine.MaxSlots)
	}
	return maxUpgradeSlots
}

// applyUpgrade recomputes a group's machine count for the given tier bonus and slot count
// and records the chosen upgrade on the group. n is clamped to the machine's slot cap; a
// zero bonus or zero count clears the upgrade fields. recipeRate is in recipes/tick.
func applyUpgrade(g MachineGroupDraft, recipe *RecipeRow, machine *MachineSpec, euBonusPerSlot int64, n int, recipeRate Rational) MachineGroupDraft {
	if euBonusPerSlot <= 0 || n <= 0 {
		euBonusPerSlot, n = 0, 0
	}
	if cap := upgradeSlotCap(machine); n > cap {
		n = cap
	}
	ticks := effectiveTicks(recipe, machine, euBonusPerSlot, n)
	exact := recipeRate.Mul(NewRational(ticks, 1))
	count := max(exact.CeilInt(), 1)
	g.ExactCount = exact
	g.Count = count
	g.Utilization = exact.Div(NewRational(count, 1))
	g.UpgradeCount = n
	if n == 0 {
		g.UpgradeTier = ""
	}
	return g
}

// OptimizeUpgrades applies machine upgrades to EU machine groups according to req.UpgradeMode.
// Fixed mode applies the chosen tier+count to every upgradable group; auto mode picks the
// tier+count per group that minimises machine count (ties broken by fewest upgrades).
// Must run AFTER ScaleToInteger so it operates on the final (scaled) machine counts — the
// recipe rate is derived from each group's current ExactCount, so it is scaling-independent.
// Returns updated groups and warnings; caller must authorize ownership if needed.
func (s *Solver) OptimizeUpgrades(ctx context.Context, groups []MachineGroupDraft, req SolveRequest) ([]MachineGroupDraft, []Warning, error) {
	if req.UpgradeMode != UpgradeModeFixed && req.UpgradeMode != UpgradeModeAuto {
		return groups, nil, nil
	}

	var warnings []Warning
	tiersByMod := map[string][]*UpgradeTierSpec{}

	for i := range groups {
		g := &groups[i]
		machine, err := s.DB.GetMachineType(ctx, g.MachineMod, g.MachineID)
		if err != nil {
			return nil, nil, fmt.Errorf("solver: get machine %s:%s: %w", g.MachineMod, g.MachineID, err)
		}
		if machine == nil || !machine.Upgradable || machine.EnergyType != "eu" {
			continue
		}
		recipe, err := s.DB.GetRecipe(ctx, g.RecipeID)
		if err != nil {
			return nil, nil, fmt.Errorf("solver: get recipe %s: %w", g.RecipeID, err)
		}
		if recipe == nil {
			continue
		}
		// Derive the group's recipe rate (recipes/tick) from its current exact count, which
		// reflects any AUTO scaling already applied: rate = ExactCount / currentBaseTicks.
		baseTicks := effectiveTicks(recipe, machine, 0, 0)
		recipeRate := g.ExactCount.Div(NewRational(baseTicks, 1))

		tiers, ok := tiersByMod[g.MachineMod]
		if !ok {
			tiers, err = s.DB.GetUpgradeTiers(ctx, g.MachineMod)
			if err != nil {
				return nil, nil, fmt.Errorf("solver: get upgrade tiers %s: %w", g.MachineMod, err)
			}
			tiersByMod[g.MachineMod] = tiers
		}

		switch req.UpgradeMode {
		case UpgradeModeFixed:
			tier := findTier(tiers, req.UpgradeTier)
			if tier == nil {
				warnings = append(warnings, Warning{
					Code:   "upgrade_tier_unavailable",
					Params: map[string]string{"tier": req.UpgradeTier, "mod": g.MachineMod},
				})
				continue
			}
			*g = applyUpgrade(*g, recipe, machine, tier.EUBonusPerSlot, req.UpgradeCount, recipeRate)
			g.UpgradeTier = tier.ID
		case UpgradeModeAuto:
			*g = autoUpgrade(*g, recipe, machine, filterTiers(tiers, req.AllowedTiers), recipeRate)
		}
	}

	return groups, warnings, nil
}

// findTier returns the tier with the given ID, or nil.
func findTier(tiers []*UpgradeTierSpec, id string) *UpgradeTierSpec {
	for _, t := range tiers {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// filterTiers keeps only tiers whose ID is in allowed; empty allowed keeps all.
func filterTiers(tiers []*UpgradeTierSpec, allowed []string) []*UpgradeTierSpec {
	if len(allowed) == 0 {
		return tiers
	}
	keep := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		keep[id] = true
	}
	out := make([]*UpgradeTierSpec, 0, len(tiers))
	for _, t := range tiers {
		if keep[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

// autoUpgrade chooses the tier+count that minimises machine count for the group; ties are
// broken by the fewest upgrade slots (the knee point — extra upgrades that don't reduce
// machine count are wasteful). Returns the group unchanged if no tier helps.
func autoUpgrade(g MachineGroupDraft, recipe *RecipeRow, machine *MachineSpec, tiers []*UpgradeTierSpec, recipeRate Rational) MachineGroupDraft {
	best := g // base case (no upgrade) already computed in CalculateMachineGroups
	bestCount := g.Count
	bestN := 0
	cap := upgradeSlotCap(machine)
	for _, t := range tiers {
		if t.EUBonusPerSlot <= 0 {
			continue
		}
		for n := 1; n <= cap; n++ {
			cand := applyUpgrade(g, recipe, machine, t.EUBonusPerSlot, n, recipeRate)
			cand.UpgradeTier = t.ID
			// Prefer fewer machines; on ties prefer fewer upgrade slots. The base case
			// (bestN==0) wins any tie because no candidate has n < 1.
			if cand.Count < bestCount || (cand.Count == bestCount && bestN > 0 && n < bestN) {
				best, bestCount, bestN = cand, cand.Count, n
			}
		}
	}
	return best
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

// ceilDiv returns the ceiling of a/b for positive b.
func ceilDiv(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

