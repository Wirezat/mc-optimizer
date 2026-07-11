package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// groupSpecs resolves the machine spec, recipe, and per-slot upgrade bonus (of the
// group's own target tier) needed by both EU enrichment and throughput estimation.
// The bonus is 0 when the group has no upgrade tier.
func (d *DB) groupSpecs(ctx context.Context, mg *model.MachineGroup) (*solver.MachineSpec, *solver.RecipeRow, int64, error) {
	machine, err := d.GetMachineType(ctx, mg.MachineModID, mg.MachineID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("db: group specs: get machine type: %w", err)
	}
	recipe, err := d.GetRecipe(ctx, mg.RecipeID.String())
	if err != nil {
		return nil, nil, 0, fmt.Errorf("db: group specs: get recipe: %w", err)
	}
	var bonus int64
	if mg.UpgradeTierID != nil {
		tiers, err := d.GetUpgradeTiers(ctx, mg.MachineModID)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("db: group specs: get upgrade tiers: %w", err)
		}
		for _, t := range tiers {
			if t.ID == mg.UpgradeTierID.String() {
				bonus = t.EUBonusPerSlot
				break
			}
		}
	}
	return machine, recipe, bonus, nil
}

// EnrichMachineGroupEU populates a machine group's computed EU fields: EUPerTick (per
// machine at the target upgrade loadout) and CurrentEUPerTick (per machine at the
// currently installed upgrade count — same tier, since upgrades always track toward
// the group's own target tier). These are computed on read, not persisted; non-EU
// machines are left at zero.
func (d *DB) EnrichMachineGroupEU(ctx context.Context, mg *model.MachineGroup) error {
	machine, recipe, bonus, err := d.groupSpecs(ctx, mg)
	if err != nil {
		return err
	}
	if machine == nil || machine.EnergyType != "eu" {
		return nil
	}
	mg.EUPerTick = solver.EffectiveEUPerTick(recipe, machine, bonus, mg.UpgradeCount)
	mg.CurrentEUPerTick = solver.EffectiveEUPerTick(recipe, machine, bonus, mg.CurrentUpgradeCount)
	return nil
}

// EstimateCurrentRateFraction returns the fraction (0..1) of a production line's target
// output achievable with the groups' current build state — the minimum over machine
// groups of currentThroughput/targetThroughput, since a chain's output is limited by
// its slowest link. Missing upgrades slow machines down (longer effective ticks), so
// the fraction accounts for both fewer machines and a weaker upgrade loadout.
func (d *DB) EstimateCurrentRateFraction(ctx context.Context, groups []*model.MachineGroup) (float64, error) {
	frac := 1.0
	for _, mg := range groups {
		machine, recipe, bonus, err := d.groupSpecs(ctx, mg)
		if err != nil {
			return 0, err
		}

		// Target throughput uses the lossless fractional machine count (recipes/tick =
		// exactCount / targetTicks); fall back to the rounded count if it's unset.
		exact := float64(mg.Count)
		if mg.ExactCountDen > 0 && mg.ExactCountNum > 0 {
			exact = float64(mg.ExactCountNum) / float64(mg.ExactCountDen)
		}
		if exact <= 0 {
			continue
		}

		targetTicks := solver.EffectiveTicks(recipe, machine, bonus, mg.UpgradeCount)
		currentTicks := solver.EffectiveTicks(recipe, machine, bonus, mg.CurrentUpgradeCount)
		if targetTicks <= 0 || currentTicks <= 0 {
			continue
		}

		g := (float64(mg.BuiltCount) / float64(currentTicks)) / (exact / float64(targetTicks))
		if g > 1 {
			g = 1 // rounding count up can overshoot the exact requirement
		}
		if g < frac {
			frac = g
		}
	}
	return frac, nil
}
