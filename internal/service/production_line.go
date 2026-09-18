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
type draftPayload struct {
	Request solver.SolveRequest `json:"request"`
	Result  solver.SolveResult  `json:"result"`
}

// PLService coordinates solving and confirming production lines.
type PLService struct {
	db           *db.DB
	autoScaleMax int64
	variants     solver.VariantSource
}

// NewPLService creates a PLService with the given database, auto-scale cap and variant
// source.
func NewPLService(database *db.DB, autoScaleMax int64, variants solver.VariantSource) *PLService {
	return &PLService{db: database, autoScaleMax: autoScaleMax, variants: variants}
}

// solverFor builds a solver bound to a factory's active mods.
func (s *PLService) solverFor(ctx context.Context, factoryID uuid.UUID, req *solver.SolveRequest) (*solver.Solver, error) {
	factory, err := s.db.GetFactory(ctx, factoryID)
	if err != nil {
		return nil, err
	}
	activeMods, err := s.db.GetActiveMods(ctx, factory.SaveID)
	if err != nil {
		return nil, err
	}
	sv := solver.NewSolver(s.db, s.autoScaleMax)
	sv.ActiveMods = activeMods
	sv.VariantSource = s.variants
	return sv, nil
}

// SolveOutput is returned by Solve and contains what the HTTP handler needs.
type SolveOutput struct {
	DraftID   uuid.UUID          `json:"draft_id"`
	ExpiresAt time.Time          `json:"expires_at"`
	Result    solver.SolveResult `json:"result"`
}

// Solve runs the solver with the given request, persists a draft, and returns the result.
func (s *PLService) Solve(ctx context.Context, factoryID, userID uuid.UUID, req solver.SolveRequest) (*SolveOutput, error) {
	GoLog.Infof("solve: target=%s:%s mode=%s rate=%d/%d unit=%s stops=%d",
		req.TargetItem.ModID, req.TargetItem.ItemID,
		req.Mode, req.TargetRate.Num, req.TargetRate.Den, req.TimeUnit,
		len(req.StopPoints))

	sv, err := s.solverFor(ctx, factoryID, &req)
	if err != nil {
		return nil, err
	}
	result, err := sv.Solve(ctx, req)
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

// ConfirmInput carries the parsed body from the confirm endpoint.
type ConfirmInput struct {
	DraftID uuid.UUID
}

// Confirm promotes a solver draft to a live production line. Returns ErrDraftNotFound if
// the draft is missing or expired.
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

	ios, groups, err := solveResultToContents(payload.Result, payload.Request.ModConfigs)
	if err != nil {
		return nil, err
	}

	return s.db.ConfirmSolverDraft(ctx, pl, ios, groups, input.DraftID)
}

// solveResultToContents converts a solver result into the persistable PLIO and MachineGroup
// rows.
func solveResultToContents(result solver.SolveResult, modConfigs map[string]json.RawMessage) ([]*model.PLIO, []*model.MachineGroup, error) {
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
		exactDen := mg.ExactCount.Den
		if exactDen == 0 {
			exactDen = 1
		}
		groups = append(groups, &model.MachineGroup{
			MachineModID:  mg.MachineMod,
			MachineID:     mg.MachineID,
			RecipeID:      recipeID,
			Count:         int(mg.Count),
			Status:        "planned",
			VariantID:     mg.VariantID,
			ModConfig:     modConfigs[mg.PluginMod],
			ExactCountNum: mg.ExactCount.Num,
			ExactCountDen: exactDen,
			Costs:         mg.Costs,
		})
	}
	return ios, groups, nil
}

// Scale resizes a saved line to k times its machines and rates, in place, so status and
// built counts survive. Returns ErrBuiltCountExceeded when a group would end up smaller
// than what is already built.
func (s *PLService) Scale(ctx context.Context, plID uuid.UUID, k solver.Rational) (*model.ProductionLineDetail, error) {
	if k.Den == 0 || !k.IsPositive() {
		return nil, fmt.Errorf("service: scale: factor must be positive")
	}
	pl, err := s.db.GetProductionLine(ctx, plID)
	if err != nil {
		return nil, err
	}
	groups, err := s.db.ListMachineGroupsByPL(ctx, plID)
	if err != nil {
		return nil, err
	}
	ios, err := s.db.ListPLIOByPL(ctx, plID)
	if err != nil {
		return nil, err
	}

	rateNum, rateDen, scaledGroups, scaledIOs, err := scalePLRows(pl.RateNum, pl.RateDen, groups, ios, k)
	if err != nil {
		return nil, err
	}
	if err := s.db.ApplyPLScale(ctx, plID, rateNum, rateDen, scaledGroups, scaledIOs); err != nil {
		return nil, err
	}

	pl, err = s.db.GetProductionLine(ctx, plID)
	if err != nil {
		return nil, err
	}
	groups, err = s.db.ListMachineGroupsByPL(ctx, plID)
	if err != nil {
		return nil, err
	}
	ios, err = s.db.ListPLIOByPL(ctx, plID)
	if err != nil {
		return nil, err
	}
	return &model.ProductionLineDetail{ProductionLine: *pl, MachineGroups: groups, IO: ios}, nil
}
