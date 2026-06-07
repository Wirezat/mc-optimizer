package solver

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// RecipeStore is the data-access interface required by the solver.
// db.DB satisfies this interface implicitly.
type RecipeStore interface {
	GetRecipesForItem(ctx context.Context, modID, itemID string) ([]*model.RecipeRow, error)
	GetRecipe(ctx context.Context, id string) (*model.RecipeRow, error)
	GetMachineType(ctx context.Context, modID, machineID string) (*model.MachineType, error)
	GetUpgradeTiers(ctx context.Context, modID string) ([]*model.UpgradeTier, error)
}

// Solver executes production line optimization.
type Solver struct {
	DB           RecipeStore
	AutoScaleMax int64
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

	g, err := s.BuildRecipeGraph(ctx, req.TargetItem, req.StopPoints, req.FactoryState, req.RecipeOverrides)
	if err != nil {
		return SolveResult{}, fmt.Errorf("solver: build recipe graph: %w", err)
	}
	if len(g.Nodes) == 0 {
		return SolveResult{}, fmt.Errorf("solver: no nodes in recipe graph for %s", req.TargetItem.Key())
	}

	var warnings []string
	hadCycles := DetectCycles(g)

	var rv RateVector
	if !hadCycles {
		rv, err = SolveDAG(g, targetRate)
		if err != nil {
			return SolveResult{}, fmt.Errorf("solver: dag solve: %w", err)
		}
	} else {
		warnings = append(warnings, "cycle detected — using linear system solver")
		rv, err = SolveLinearSystem(g, targetRate)
		if err != nil {
			return SolveResult{}, fmt.Errorf("solver: linear system solve: %w", err)
		}
	}

	groups, err := s.CalculateMachineGroups(ctx, g, rv)
	if err != nil {
		return SolveResult{}, fmt.Errorf("solver: calculate machine groups: %w", err)
	}

	groups, upgradeWarns, err := s.OptimizeUpgrades(ctx, groups, rv)
	if err != nil {
		return SolveResult{}, fmt.Errorf("solver: optimize upgrades: %w", err)
	}
	warnings = append(warnings, upgradeWarns...)

	actualRate := targetRate
	if req.Mode == SolveModeAuto {
		var scaleWarns []string
		groups, actualRate, scaleWarns = s.ScaleToInteger(groups, rv, req.TargetItem, s.AutoScaleMax)
		warnings = append(warnings, scaleWarns...)
	}

	return SolveResult{
		MachineGroups: groups,
		IOProfile:     ComputeIOProfile(rv, g, req.FactoryState, req.TimeUnit),
		ActualRate:    actualRate,
		HadCycles:     hadCycles,
		ModeUsed:      req.Mode,
		Warnings:      warnings,
	}, nil
}
