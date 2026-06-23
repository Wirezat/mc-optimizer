package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/fracidx"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

var (
	ErrDraftNotFound   = errors.New("draft not found or expired")
	ErrWrongFactory    = errors.New("draft belongs to a different factory")
	ErrNoStoredRequest = errors.New("production line has no stored solver request")
)

// draftPayload is the JSON structure stored in solver_drafts.result.
// It bundles the original request alongside the computed result so the
// confirm step can reconstruct the production line without re-running the solver.
type draftPayload struct {
	Request solver.SolveRequest `json:"request"`
	Result  solver.SolveResult  `json:"result"`
}

// PLService coordinates solving and confirming production lines.
// It is the single point of business logic between HTTP handlers and the solver/DB.
type PLService struct {
	db           *db.DB
	autoScaleMax int64
}

// NewPLService creates a PLService with the given database and auto-scale cap.
func NewPLService(database *db.DB, autoScaleMax int64) *PLService {
	return &PLService{db: database, autoScaleMax: autoScaleMax}
}

// SolveOutput is returned by Solve and contains what the HTTP handler needs.
type SolveOutput struct {
	DraftID   uuid.UUID        `json:"draft_id"`
	ExpiresAt time.Time        `json:"expires_at"`
	Result    solver.SolveResult `json:"result"`
}

// Solve runs the solver with the given request, persists a draft, and returns the result.
// Stop points and factory state are provided directly by the caller (no server-side merging).
func (s *PLService) Solve(ctx context.Context, factoryID, userID uuid.UUID, req solver.SolveRequest) (*SolveOutput, error) {
	GoLog.Infof("solve: target=%s:%s mode=%s rate=%d/%d unit=%s stops=%d",
		req.TargetItem.ModID, req.TargetItem.ItemID,
		req.Mode, req.TargetRate.Num, req.TargetRate.Den, req.TimeUnit,
		len(req.StopPoints))

	result, err := solver.NewSolver(s.db, s.autoScaleMax).Solve(ctx, req)
	if err != nil {
		GoLog.Infof("solve: error: %v", err)
		return nil, err
	}
	GoLog.Infof("solve: ok: mode=%s rate=%d/%d groups=%d warnings=%d",
		result.ModeUsed, result.ActualRate.Num, result.ActualRate.Den,
		len(result.MachineGroups), len(result.Warnings))

	payloadJSON, err := json.Marshal(draftPayload{Request: req, Result: result})
	if err != nil {
		return nil, fmt.Errorf("service: marshal draft: %w", err)
	}
	draft, err := s.db.CreateSolverDraft(ctx, factoryID, userID, payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("service: create draft: %w", err)
	}

	return &SolveOutput{
		DraftID:   draft.ID,
		ExpiresAt: draft.ExpiresAt,
		Result:    result,
	}, nil
}

// SetGroupUpgrade applies an upgrade tier/count to a single machine group and recomputes
// its machine count while preserving the group's current output rate (variant A: more
// upgrades → fewer machines, no PL-wide rebalancing). A nil tierID or count<=0 clears the
// upgrade. Returns the new machine count.
func (s *PLService) SetGroupUpgrade(ctx context.Context, groupID uuid.UUID, tierID *uuid.UUID, upgradeCount int) (int, error) {
	grp, err := s.db.GetMachineGroup(ctx, groupID)
	if err != nil {
		return 0, err
	}
	recipe, err := s.db.GetRecipe(ctx, grp.RecipeID.String())
	if err != nil {
		return 0, fmt.Errorf("service: get recipe: %w", err)
	}
	machine, err := s.db.GetMachineType(ctx, grp.MachineModID, grp.MachineID)
	if err != nil {
		return 0, fmt.Errorf("service: get machine: %w", err)
	}
	tiers, err := s.db.GetUpgradeTiers(ctx, grp.MachineModID)
	if err != nil {
		return 0, fmt.Errorf("service: get tiers: %w", err)
	}
	bonusOf := func(id string) int64 {
		for _, t := range tiers {
			if t.ID == id {
				return t.EUBonusPerSlot
			}
		}
		return 0
	}

	// Normalise the requested upgrade: an unknown tier or non-positive count clears it.
	newTierStr := ""
	if tierID != nil {
		newTierStr = tierID.String()
	}
	if upgradeCount <= 0 || tierID == nil || bonusOf(newTierStr) == 0 {
		tierID, upgradeCount, newTierStr = nil, 0, ""
	}

	oldTierStr := ""
	if grp.UpgradeTierID != nil {
		oldTierStr = grp.UpgradeTierID.String()
	}

	ticksOld := solver.EffectiveTicks(recipe, machine, bonusOf(oldTierStr), grp.UpgradeCount)
	ticksNew := solver.EffectiveTicks(recipe, machine, bonusOf(newTierStr), upgradeCount)

	// Recompute from the fractional exact count, preserving the group's required recipe rate
	// (rate = exactOld / ticksOld). This is lossless and round-trip stable, unlike deriving
	// from the rounded count. Legacy rows (no exact stored) fall back to the rounded count.
	exactOld := solver.NewRational(grp.ExactCountNum, grp.ExactCountDen)
	if grp.ExactCountNum == 0 {
		exactOld = solver.NewRational(int64(grp.Count), 1)
	}
	newExact := exactOld
	if ticksOld > 0 {
		// newExact = exactOld × ticksNew / ticksOld
		newExact = exactOld.Mul(solver.NewRational(ticksNew, 1)).Div(solver.NewRational(ticksOld, 1))
	}
	newCount := max(int(newExact.CeilInt()), 1)

	if err := s.db.UpdateMachineGroupUpgrades(ctx, groupID, tierID, upgradeCount, newCount, newExact.Num, newExact.Den); err != nil {
		return 0, err
	}
	return newCount, nil
}

// ConfirmInput carries the parsed body from the confirm endpoint.
type ConfirmInput struct {
	DraftID uuid.UUID
	Name    string
}

// Confirm promotes a solver draft to a live production line.
// Returns ErrDraftNotFound if the draft is missing or expired.
// Returns ErrWrongFactory if the draft was created for a different factory.
func (s *PLService) Confirm(ctx context.Context, factoryID uuid.UUID, input ConfirmInput) (*model.ProductionLineDetail, error) {
	draft, err := s.db.GetSolverDraft(ctx, input.DraftID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrDraftNotFound
		}
		return nil, fmt.Errorf("service: get draft: %w", err)
	}
	if draft.FactoryID != factoryID {
		return nil, ErrWrongFactory
	}

	var payload draftPayload
	if err := json.Unmarshal(draft.Result, &payload); err != nil {
		return nil, fmt.Errorf("service: unmarshal draft: %w", err)
	}

	lastPos, err := s.db.GetLastPLPosition(ctx, factoryID, nil)
	if err != nil {
		return nil, fmt.Errorf("service: get last pl position: %w", err)
	}

	reqJSON, err := json.Marshal(payload.Request)
	if err != nil {
		return nil, fmt.Errorf("service: marshal solve request: %w", err)
	}
	pl := &model.ProductionLine{
		FactoryID:    &factoryID,
		Name:         input.Name,
		TargetModID:  payload.Request.TargetItem.ModID,
		TargetItemID: payload.Request.TargetItem.ItemID,
		RateNum:      int(payload.Request.TargetRate.Num),
		RateDen:      int(payload.Request.TargetRate.Den),
		TimeUnit:     payload.Request.TimeUnit,
		OptimizeMode: string(payload.Request.Mode),
		Status:       "active",
		Position:     fracidx.Between(lastPos, ""),
		SolveRequest: reqJSON,
	}

	ios, groups, err := solveResultToContents(payload.Result)
	if err != nil {
		return nil, err
	}

	return s.db.ConfirmSolverDraft(ctx, pl, ios, groups, input.DraftID)
}

// solveResultToContents converts a solver result into the persistable PLIO and MachineGroup
// rows. New groups are returned with status "planned". Shared by Confirm and Resolve.
func solveResultToContents(result solver.SolveResult) ([]*model.PLIO, []*model.MachineGroup, error) {
	totalIO := len(result.IOProfile.Inputs) + len(result.IOProfile.Outputs)
	ios := make([]*model.PLIO, 0, totalIO)
	ioType := func(item solver.ItemRef) string {
		if item.IsFluid {
			return "fluid"
		}
		return "item"
	}
	for _, entry := range result.IOProfile.Inputs {
		ios = append(ios, &model.PLIO{
			Direction:   "input",
			IOType:      ioType(entry.Item),
			ModID:       entry.Item.ModID,
			ItemFluidID: entry.Item.ItemID,
			RateNum:     int(entry.Rate.Num),
			RateDen:     int(entry.Rate.Den),
			IsStopPoint: entry.IsStopPoint,
		})
	}
	for _, entry := range result.IOProfile.Outputs {
		ios = append(ios, &model.PLIO{
			Direction:   "output",
			IOType:      ioType(entry.Item),
			ModID:       entry.Item.ModID,
			ItemFluidID: entry.Item.ItemID,
			RateNum:     int(entry.Rate.Num),
			RateDen:     int(entry.Rate.Den),
		})
	}

	groups := make([]*model.MachineGroup, 0, len(result.MachineGroups))
	for i, mg := range result.MachineGroups {
		recipeID, err := uuid.Parse(mg.RecipeID)
		if err != nil {
			return nil, nil, fmt.Errorf("service: machine group %d: invalid recipe UUID %q: %w", i, mg.RecipeID, err)
		}
		var tierID *uuid.UUID
		if mg.UpgradeTier != "" {
			id, err := uuid.Parse(mg.UpgradeTier)
			if err != nil {
				return nil, nil, fmt.Errorf("service: machine group %d: invalid upgrade tier UUID %q: %w", i, mg.UpgradeTier, err)
			}
			tierID = &id
		}
		exactDen := mg.ExactCount.Den
		if exactDen == 0 {
			exactDen = 1
		}
		groups = append(groups, &model.MachineGroup{
			MachineModID:  mg.MachineMod,
			MachineID:     mg.MachineID,
			RecipeID:      recipeID,
			Count:         int(mg.Count),
			UpgradeTierID: tierID,
			UpgradeCount:  mg.UpgradeCount,
			Status:        "planned",
			ExactCountNum: mg.ExactCount.Num,
			ExactCountDen: exactDen,
		})
	}
	return ios, groups, nil
}

// ResolveInput carries optional overrides for re-solving a production line. Zero/empty
// fields fall back to the line's stored request.
type ResolveInput struct {
	TargetRate   *solver.Rational
	TimeUnit     string
	UpgradeMode  string
	UpgradeTier  string
	UpgradeCount int
}

// Resolve re-solves an existing production line (e.g. to produce more output) using its
// stored solver request, with the given overrides applied, and replaces its machine groups
// and IO in place. The line keeps its id, name, position, and status; new groups are
// "planned" (build progress resets). Returns the updated detail.
func (s *PLService) Resolve(ctx context.Context, plID uuid.UUID, in ResolveInput) (*model.ProductionLineDetail, error) {
	raw, err := s.db.GetPLSolveRequest(ctx, plID)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, ErrNoStoredRequest
	}
	var req solver.SolveRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("service: unmarshal stored request: %w", err)
	}

	if in.TargetRate != nil && in.TargetRate.Den != 0 {
		req.TargetRate = *in.TargetRate
	}
	if in.TimeUnit != "" {
		req.TimeUnit = in.TimeUnit
	}
	if in.UpgradeMode != "" {
		req.UpgradeMode = solver.UpgradeMode(in.UpgradeMode)
		req.UpgradeTier = in.UpgradeTier
		req.UpgradeCount = in.UpgradeCount
	}

	result, err := solver.NewSolver(s.db, s.autoScaleMax).Solve(ctx, req)
	if err != nil {
		return nil, err
	}

	ios, groups, err := solveResultToContents(result)
	if err != nil {
		return nil, err
	}

	return s.db.ReplaceProductionLineContents(ctx, plID,
		int(req.TargetRate.Num), int(req.TargetRate.Den), req.TimeUnit, string(req.Mode),
		ios, groups)
}
