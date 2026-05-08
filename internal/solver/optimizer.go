package solver

import (
	"context"
	"fmt"
	"math"

	"github.com/Wirezat/production-optimizer/internal/db"
)

func CalculateMachineGroups(ctx context.Context, database *db.DB, g *RecipeGraph, rv RateVector) ([]MachineGroupDraft, error) {
	var groups []MachineGroupDraft

	for recipeID, recipeRate := range rv.RecipeRates {
		if recipeRate.IsZero() {
			continue
		}
		recipe, err := database.GetRecipe(ctx, recipeID)
		if err != nil {
			return nil, fmt.Errorf("get recipe %s: %w", recipeID, err)
		}
		machine, err := database.GetMachineType(ctx, recipe.MachineMod, recipe.MachineID)
		if err != nil {
			return nil, fmt.Errorf("get machine %s:%s: %w", recipe.MachineMod, recipe.MachineID, err)
		}

		effEU := max(machine.BaseEUPerTick, 1)
		ticksPerRecipe := max(ceildiv(recipe.TotalEU, effEU), 1)

		exact := recipeRate.Mul(NewRational(ticksPerRecipe, 1))
		count := max(exact.CeilInt(), 1)

		groups = append(groups, MachineGroupDraft{
			MachineMod:  machine.ModID,
			MachineID:   machine.MachineID,
			RecipeID:    recipeID,
			Count:       count,
			Utilization: exact.Div(NewRational(count, 1)),
			Status:      StatusDraft,
		})
	}

	return groups, nil
}

func OptimizeUpgrades(ctx context.Context, database *db.DB, groups []MachineGroupDraft, rv RateVector) ([]MachineGroupDraft, []string, error) {
	var warnings []string
	result := make([]MachineGroupDraft, len(groups))
	copy(result, groups)

	for i, group := range result {
		machine, err := database.GetMachineType(ctx, group.MachineMod, group.MachineID)
		if err != nil {
			return nil, nil, fmt.Errorf("get machine %s:%s: %w", group.MachineMod, group.MachineID, err)
		}
		if machine.EnergyType != "EU" {
			continue
		}
		recipe, err := database.GetRecipe(ctx, group.RecipeID)
		if err != nil {
			return nil, nil, fmt.Errorf("get recipe %s: %w", group.RecipeID, err)
		}
		recipeRate, ok := rv.RecipeRates[group.RecipeID]
		if !ok || recipeRate.IsZero() {
			continue
		}

		// ticks available per machine per recipe cycle
		ticksNeeded := max(NewRational(group.Count, 1).Div(recipeRate).CeilInt(), 1)
		requiredEffEU := ceildiv(recipe.TotalEU, ticksNeeded)
		if requiredEffEU <= machine.BaseEUPerTick {
			continue
		}

		tiers, err := database.GetUpgradeTiers(ctx, machine.ModID)
		if err != nil {
			return nil, nil, fmt.Errorf("get upgrade tiers for %s: %w", machine.ModID, err)
		}

		found := false
		for _, tier := range tiers {
			upgradeCount := ceildiv(requiredEffEU-machine.BaseEUPerTick, tier.EUBonusPerSlot)
			if upgradeCount > int64(machine.MaxSlots) {
				continue
			}
			if min(machine.BaseEUPerTick+upgradeCount*tier.EUBonusPerSlot, machine.MaxEUPerTick) >= requiredEffEU {
				result[i].UpgradeTier = tier.ID
				result[i].UpgradeCount = int(upgradeCount)
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, fmt.Sprintf(
				"machine %s:%s cannot reach required EU/t with any upgrade tier — consider increasing machine count",
				group.MachineMod, group.MachineID,
			))
		}
	}

	return result, warnings, nil
}

func ScaleToInteger(groups []MachineGroupDraft, rv RateVector, rootItem ItemRef, maxScale int64) ([]MachineGroupDraft, Rational, []string) {
	var warnings []string

	if len(rv.RecipeRates) == 0 {
		return groups, NewRational(0, 1), warnings
	}

	fractions := make([]Rational, 0, len(rv.RecipeRates))
	for _, r := range rv.RecipeRates {
		fractions = append(fractions, r)
	}

	k := lcmOfFractions(fractions)
	if k > maxScale {
		const maxDen = int64(20)
		maxErrPct := 0.0
		for i, f := range fractions {
			approx := ApproximateByContinuedFractions(f, maxDen)
			fractions[i] = approx
			if orig := float64(f.Num) / float64(f.Den); orig > 0 {
				if e := math.Abs(orig-float64(approx.Num)/float64(approx.Den)) / orig * 100; e > maxErrPct {
					maxErrPct = e
				}
			}
		}
		k = lcmOfFractions(fractions)
		warnings = append(warnings, fmt.Sprintf(
			"AUTO: scale factor exceeded %d, continued fractions approximation used (max error: %.2f%%)",
			maxScale, maxErrPct,
		))
	}

	result := make([]MachineGroupDraft, len(groups))
	copy(result, groups)
	for i := range result {
		result[i].Count *= k
	}

	return result, rv.ItemRates[rootItem.Key()].Mul(NewRational(k, 1)), warnings
}

func ApproximateByContinuedFractions(r Rational, maxDen int64) Rational {
	intPart := r.Num / r.Den
	frac := NewRational(r.Num-intPart*r.Den, r.Den)
	if frac.IsZero() {
		return NewRational(intPart, 1)
	}

	loNum, loDen := int64(0), int64(1)
	hiNum, hiDen := int64(1), int64(1)
	best := NewRational(0, 1)
	bestDist := frac // frac - 0/1

	for {
		medNum, medDen := loNum+hiNum, loDen+hiDen
		if medDen > maxDen {
			break
		}
		med := NewRational(medNum, medDen)
		if dist := absRat(frac.Sub(med)); dist.Cmp(bestDist) < 0 {
			best, bestDist = med, dist
		}
		switch frac.Cmp(med) {
		case -1:
			hiNum, hiDen = medNum, medDen
		case 1:
			loNum, loDen = medNum, medDen
		default:
			goto done
		}
	}
done:
	return NewRational(best.Num+intPart*best.Den, best.Den)
}

func ComputeIOProfile(rv RateVector, g *RecipeGraph, factory FactoryState, timeUnit string) IOProfile {
	var inputs, outputs []IOEntry

	for key, node := range g.Nodes {
		rate := rv.ItemRates[key]
		if rate.IsZero() {
			continue
		}
		if key == g.Root.Key() {
			outputs = append(outputs, IOEntry{Item: node.Item, Rate: rate, TimeUnit: timeUnit})
		} else if node.IsRawMaterial || node.IsStopPoint || node.IsFactoryProvided {
			inputs = append(inputs, IOEntry{Item: node.Item, Rate: rate, TimeUnit: timeUnit})
		}
	}

	return IOProfile{Inputs: inputs, Outputs: outputs}
}

func lcmOfFractions(fs []Rational) int64 {
	dens := make([]int64, len(fs))
	for i, f := range fs {
		dens[i] = f.Den
	}
	return LCM(dens)
}

func ceildiv(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

func absRat(r Rational) Rational {
	return r.Abs()
}
