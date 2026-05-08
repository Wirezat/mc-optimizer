package solver

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/db"
)

type Solver struct {
	DB           *db.DB
	AutoScaleMax int64 // default 500, overridden by ENV AUTO_SCALE_MAX
}

func NewSolver(database *db.DB, autoScaleMax int64) *Solver {
	if autoScaleMax <= 0 {
		autoScaleMax = 500
	}
	return &Solver{DB: database, AutoScaleMax: autoScaleMax}
}

func (s *Solver) Solve(ctx context.Context, req SolveRequest) (SolveResult, error) {
	targetRate := ConvertToPerTick(req.TargetRate, req.TimeUnit)

	g, err := BuildRecipeGraph(ctx, s.DB, req.TargetItem, req.StopPoints, req.FactoryState, req.RecipeOverrides)
	if err != nil {
		return SolveResult{}, fmt.Errorf("build recipe graph: %w", err)
	}
	if len(g.Nodes) == 0 {
		return SolveResult{}, fmt.Errorf("no nodes in recipe graph for %s", req.TargetItem.Key())
	}

	var warnings []string
	hadCycles := DetectCycles(g)

	var rv RateVector
	if !hadCycles {
		rv, err = SolveDAG(g, targetRate)
		if err != nil {
			return SolveResult{}, fmt.Errorf("dag solve: %w", err)
		}
	} else {
		warnings = append(warnings, "cycle detected — using linear system solver")
		rv, err = SolveLinearSystem(g, targetRate)
		if err != nil {
			return SolveResult{}, fmt.Errorf("linear system solve: %w", err)
		}
	}

	groups, err := CalculateMachineGroups(ctx, s.DB, g, rv)
	if err != nil {
		return SolveResult{}, fmt.Errorf("calculate machine groups: %w", err)
	}

	groups, upgradeWarns, err := OptimizeUpgrades(ctx, s.DB, groups, rv)
	if err != nil {
		return SolveResult{}, fmt.Errorf("optimize upgrades: %w", err)
	}
	warnings = append(warnings, upgradeWarns...)

	actualRate := targetRate
	if req.Mode == SolveModeAuto {
		var scaleWarns []string
		groups, actualRate, scaleWarns = ScaleToInteger(groups, rv, req.TargetItem, s.AutoScaleMax)
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
