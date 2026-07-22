package solver

import (
	"context"
	"errors"
	"fmt"
)

// RecipeStore is the data-access interface required by the solver.
// db.DB satisfies this interface implicitly.
type RecipeStore interface {
	GetRecipesForItem(ctx context.Context, modID, itemID string) ([]*RecipeRow, error)
	GetRecipesForFluid(ctx context.Context, modID, fluidID string) ([]*RecipeRow, error)
	GetRecipe(ctx context.Context, id string) (*RecipeRow, error)
	GetMachineType(ctx context.Context, modID, machineID string) (*MachineSpec, error)
	GetUpgradeTiers(ctx context.Context, modID string) ([]*UpgradeTierSpec, error)
	GetTagMembers(ctx context.Context, tagName string) ([]ItemRef, error)
}

// Solver executes production line optimization.
type Solver struct {
	DB           RecipeStore
	AutoScaleMax int64
	ActiveMods   map[string]bool
}

// NewSolver creates a new Solver with the given store and auto-scale max.
func NewSolver(store RecipeStore, autoScaleMax int64) *Solver {
	if autoScaleMax <= 0 {
		autoScaleMax = 500
	}
	return &Solver{DB: store, AutoScaleMax: autoScaleMax}
}

// Solve computes the optimal machine groups for a production line request.
func (s *Solver) Solve(ctx context.Context, req SolveRequest) (SolveResult, error) {
	targetRate, err := ConvertToPerTick(req.TargetRate, req.TimeUnit)
	if err != nil {
		return SolveResult{}, fmt.Errorf("solver: convert rate: %w", err)
	}

	tagOverrides := req.TagOverrides
	if tagOverrides == nil {
		tagOverrides = map[string]string{}
	}
	g, err := s.BuildRecipeGraph(ctx, req.TargetItem, req.StopPoints, req.FactoryState, req.RecipeOverrides, tagOverrides)
	if err != nil {
		return SolveResult{}, fmt.Errorf("solver: build recipe graph: %w", err)
	}
	if len(g.Nodes) == 0 {
		return SolveResult{}, fmt.Errorf("solver: no nodes in recipe graph for %s", req.TargetItem.Key())
	}

	var warnings []Warning
	_, hadCycles := DetectCycles(g)

	var rv RateVector
	if !hadCycles {
		rv, err = SolveDAG(g, targetRate)
		if err != nil {
			return SolveResult{}, fmt.Errorf("solver: dag solve: %w", err)
		}
	} else {
		rv, err = SolveLinearSystem(g, targetRate)
		if err != nil {
			if !errors.Is(err, ErrNoSolution) && !errors.Is(err, ErrUnderDetermined) {
				return SolveResult{}, fmt.Errorf("solver: linear system solve: %w", err)
			}
			cycleKeys := CyclicNodeKeys(g)
			rootKey := req.TargetItem.Key()
			filtered := make([]string, 0, len(cycleKeys))
			for _, k := range cycleKeys {
				if k != rootKey {
					filtered = append(filtered, k)
				}
			}
			return SolveResult{}, &ErrCycleBreakNeeded{CycleNodes: filtered}
		}
	}

	groups, err := s.CalculateMachineGroups(ctx, g, rv)
	if err != nil {
		return SolveResult{}, fmt.Errorf("solver: calculate machine groups: %w", err)
	}

	actualRatePerTick := targetRate
	if req.Mode == SolveModeAuto {
		allowPartial := make(map[string]bool, len(req.AllowPartialMachines))
		for _, id := range req.AllowPartialMachines {
			allowPartial[id] = true
		}
		var scaleWarns []Warning
		var k int64
		groups, actualRatePerTick, k, scaleWarns = s.ScaleToInteger(groups, rv, req.TargetItem, s.AutoScaleMax, allowPartial)
		warnings = append(warnings, scaleWarns...)
		if k > 1 {
			kRat := NewRational(k, 1)
			scaled := make(map[string]Rational, len(rv.ItemRates))
			for key, rate := range rv.ItemRates {
				scaled[key] = rate.Mul(kRat)
			}
			rv.ItemRates = scaled
		}
		// Sync rv.ItemRates with actualRatePerTick: ScaleToInteger may apply GCD reduction
		// that lowers actualRatePerTick below the k-scaled value. Bring all rates in line so
		// ComputeIOProfile reflects true production rates, not the pre-GCD target rates.
		if rootRate, ok := rv.ItemRates[req.TargetItem.Key()]; ok && !rootRate.IsZero() {
			if rootRate.Num != actualRatePerTick.Num || rootRate.Den != actualRatePerTick.Den {
				scale := actualRatePerTick.Div(rootRate)
				for key, rate := range rv.ItemRates {
					rv.ItemRates[key] = rate.Mul(scale)
				}
			}
		}
	}

	// Upgrades run after scaling so they operate on the final machine counts (otherwise AUTO
	// scaling would wash out any reduction). Rate is derived per group from its ExactCount.
	groups, upgradeWarns, err := s.OptimizeUpgrades(ctx, groups, req)
	if err != nil {
		return SolveResult{}, fmt.Errorf("solver: optimize upgrades: %w", err)
	}
	warnings = append(warnings, upgradeWarns...)

	return SolveResult{
		MachineGroups:  groups,
		IOProfile:      ComputeIOProfile(rv, g, req.FactoryState, req.TimeUnit),
		ActualRate:     ConvertFromPerTick(actualRatePerTick, req.TimeUnit),
		HadCycles:      hadCycles,
		ModeUsed:       req.Mode,
		Warnings:       warnings,
		TagResolutions: g.TagResolutions,
	}, nil
}
