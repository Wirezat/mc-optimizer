package solver

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// RecipeStore is the data-access interface required by the solver.
// db.DB satisfies this interface implicitly.
type RecipeStore interface {
	GetRecipesForItem(ctx context.Context, modID, itemID string) ([]*RecipeRow, error)
	GetRecipesForFluid(ctx context.Context, modID, fluidID string) ([]*RecipeRow, error)
	GetRecipe(ctx context.Context, id string) (*RecipeRow, error)
	GetMachinesForRecipe(ctx context.Context, recipeID string) ([]MachineRef, error)
	GetMachineType(ctx context.Context, modID, machineID string) (*MachineSpec, error)
	GetTagMembers(ctx context.Context, tagName string) ([]ItemRef, error)
}

// Solver executes production line optimization.
type Solver struct {
	DB           RecipeStore
	AutoScaleMax int64
	ActiveMods   map[string]bool
	// VariantSource evaluates the operating variants of a (machine, recipe)
	// pair. Nil means every machine runs at its nominal recipe duration.
	VariantSource VariantSource
}

// NewSolver creates a new Solver with the given store and auto-scale max.
func NewSolver(store RecipeStore, autoScaleMax int64) *Solver {
	if autoScaleMax <= 0 {
		autoScaleMax = 500
	}
	return &Solver{DB: store, AutoScaleMax: autoScaleMax}
}

// Solve computes the optimal machine groups for a production line request.
// The whole body runs under guardRateArithmetic: a variant's output overrides
// become operands of every rate computation that follows, so the guard spans
// the DAG walk, the linear system, the machine counts, the integer scaling and
// the IO profile.
func (s *Solver) Solve(ctx context.Context, req SolveRequest) (res SolveResult, err error) {
	defer guardRateArithmetic(&err)
	return s.solve(ctx, req)
}

// solve is Solve's body; call it only through Solve, which installs the guard.
func (s *Solver) solve(ctx context.Context, req SolveRequest) (SolveResult, error) {
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

	// A chosen variant may override the recipe's output amounts, which changes the
	// rates, which can change which variant wins. Iterate to a fixed point;
	// MaxVariantIterations bounds a pair of variants that keep displacing each
	// other. The graph, the rates and the groups always describe the same state
	// when the loop ends, so the last round is never applied unchecked.
	baseline := captureOutputBaseline(g)
	var rv RateVector
	var groups []MachineGroupDraft
	var groupWarnings []Warning
	for round := 0; ; round++ {
		rv, err = s.solveRates(g, targetRate, hadCycles, req.TargetItem)
		if err != nil {
			return SolveResult{}, err
		}
		groups, groupWarnings, err = s.CalculateMachineGroups(ctx, g, rv, req)
		if err != nil {
			return SolveResult{}, fmt.Errorf("solver: calculate machine groups: %w", err)
		}
		if round == MaxVariantIterations {
			if syncVariantOutputs(g, baseline, groups, false) {
				warnings = append(warnings, Warning{
					Code:   "variant_not_converged",
					Params: map[string]string{"iterations": strconv.Itoa(MaxVariantIterations)},
				})
			}
			break
		}
		if !syncVariantOutputs(g, baseline, groups, true) {
			break
		}
	}
	warnings = append(warnings, groupWarnings...)

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
		groups = repickVariants(groups, req, &ladderCtx{
			affinity: modAffinity(groups),
			yields:   indexYields(g, groups),
		})
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

// solveRates computes the rate vector for the graph as it currently stands,
// walking the DAG or solving the linear system depending on hadCycles. Returns
// *ErrCycleBreakNeeded when a cycle needs a user-chosen stop point.
func (s *Solver) solveRates(g *RecipeGraph, targetRate Rational, hadCycles bool, root ItemRef) (RateVector, error) {
	if !hadCycles {
		rv, err := SolveDAG(g, targetRate)
		if err != nil {
			return RateVector{}, fmt.Errorf("solver: dag solve: %w", err)
		}
		return rv, nil
	}
	rv, err := SolveLinearSystem(g, targetRate)
	if err == nil {
		return rv, nil
	}
	if !errors.Is(err, ErrNoSolution) && !errors.Is(err, ErrUnderDetermined) {
		return RateVector{}, fmt.Errorf("solver: linear system solve: %w", err)
	}
	rootKey := root.Key()
	cycleKeys := CyclicNodeKeys(g)
	filtered := make([]string, 0, len(cycleKeys))
	for _, k := range cycleKeys {
		if k != rootKey {
			filtered = append(filtered, k)
		}
	}
	return RateVector{}, &ErrCycleBreakNeeded{CycleNodes: filtered}
}
