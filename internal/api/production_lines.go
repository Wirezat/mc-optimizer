package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

// SolveHandler runs the solver for a factory and stores a solver_draft.
// POST /api/factories/{factory_id}/solve
func SolveHandler(database *db.DB, autoScaleMax int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		var req solver.SolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if req.TargetItem.ModID == "" || req.TargetItem.ItemID == "" {
			errBadRequest(w, "target_item requires mod_id and item_id")
			return
		}
		if req.TimeUnit == "" {
			errBadRequest(w, "time_unit is required")
			return
		}
		if req.TargetRate.Den == 0 {
			errBadRequest(w, "target_rate is required")
			return
		}
		s := solver.NewSolver(database, autoScaleMax)
		result, err := s.Solve(r.Context(), req)
		if err != nil {
			errInternal(w, err)
			return
		}
		payload := solver.DraftPayload{
			Request: req,
			Result:  result,
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			errInternal(w, err)
			return
		}
		draft, err := database.CreateSolverDraft(r.Context(), factoryID, userID, payloadJSON)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"draft_id":   draft.ID,
			"expires_at": draft.ExpiresAt,
		})
	}
}

// ConfirmProductionLineHandler promotes a solver_draft to a live production line.
// POST /api/factories/{factory_id}/production-line/confirm
//
// Body: { "draft_id": "<uuid>", "name": "<string>" }
//
// Flow:
//  1. Ownership check on factory
//  2. Load solver draft, verify it belongs to this factory
//  3. Unmarshal DraftPayload{Request, Result}
//  4. Convert solver types to model types
//  5. Persist in one transaction (PL + PLIO + MachineGroups + delete draft)
//  6. Return ProductionLineDetail
func ConfirmProductionLineHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())

		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}

		// Parse body.
		var body struct {
			DraftID string `json:"draft_id"`
			Name    string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		draftID, err := uuid.Parse(strings.TrimSpace(body.DraftID))
		if err != nil {
			errBadRequest(w, "draft_id must be a valid UUID")
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			errBadRequest(w, "name is required")
			return
		}

		// Load & validate draft.
		draft, err := database.GetSolverDraft(r.Context(), draftID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w) // also covers expired drafts
				return
			}
			errInternal(w, err)
			return
		}
		// Prevent using a draft from a different factory.
		if draft.FactoryID != factoryID {
			errForbidden(w)
			return
		}

		// Unmarshal solver payload.
		var payload solver.DraftPayload
		if err := json.Unmarshal(draft.Result, &payload); err != nil {
			errInternal(w, fmt.Errorf("confirm: unmarshal draft payload: %w", err))
			return
		}

		// Build ProductionLine.
		pl := &model.ProductionLine{
			FactoryID:    &factoryID,
			Name:         body.Name,
			TargetModID:  payload.Request.TargetItem.ModID,
			TargetItemID: payload.Request.TargetItem.ItemID,
			RateNum:      int(payload.Request.TargetRate.Num),
			RateDen:      int(payload.Request.TargetRate.Den),
			TimeUnit:     payload.Request.TimeUnit,
			OptimizeMode: string(payload.Request.Mode),
			Status:       "active",
		}

		// Build PLIO entries.
		totalIO := len(payload.Result.IOProfile.Inputs) + len(payload.Result.IOProfile.Outputs)
		ios := make([]*model.PLIO, 0, totalIO)

		for _, entry := range payload.Result.IOProfile.Inputs {
			ios = append(ios, &model.PLIO{
				Direction:   "input",
				IOType:      "item", // TODO: extend when fluids are tracked in IOEntry
				ModID:       entry.Item.ModID,
				ItemFluidID: entry.Item.ItemID,
				RateNum:     int(entry.Rate.Num),
				RateDen:     int(entry.Rate.Den),
				IsStopPoint: entry.IsStopPoint,
			})
		}
		for _, entry := range payload.Result.IOProfile.Outputs {
			ios = append(ios, &model.PLIO{
				Direction:   "output",
				IOType:      "item",
				ModID:       entry.Item.ModID,
				ItemFluidID: entry.Item.ItemID,
				RateNum:     int(entry.Rate.Num),
				RateDen:     int(entry.Rate.Den),
				IsStopPoint: false,
			})
		}

		// Build MachineGroups.
		groups := make([]*model.MachineGroup, 0, len(payload.Result.MachineGroups))
		for i, mgDraft := range payload.Result.MachineGroups {
			recipeID, err := uuid.Parse(mgDraft.RecipeID)
			if err != nil {
				errInternal(w, fmt.Errorf("confirm: machine group %d: invalid recipe UUID %q: %w", i, mgDraft.RecipeID, err))
				return
			}
			var tierID *uuid.UUID
			if mgDraft.UpgradeTier != "" {
				id, err := uuid.Parse(mgDraft.UpgradeTier)
				if err != nil {
					errInternal(w, fmt.Errorf("confirm: machine group %d: invalid upgrade tier UUID %q: %w", i, mgDraft.UpgradeTier, err))
					return
				}
				tierID = &id
			}
			groups = append(groups, &model.MachineGroup{
				MachineModID:  mgDraft.MachineMod,
				MachineID:     mgDraft.MachineID,
				RecipeID:      recipeID,
				Count:         int(mgDraft.Count),
				UpgradeTierID: tierID,
				UpgradeCount:  mgDraft.UpgradeCount,
				Status:        "planned", // draft → planned on confirm
			})
		}

		// Persist everything in one transaction.
		detail, err := database.ConfirmSolverDraft(r.Context(), pl, ios, groups, draftID)
		if err != nil {
			errInternal(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, detail)
	}
}

// ListProductionLinesHandler returns all production lines for a factory.
// GET /api/factories/{factory_id}/production-lines
func ListProductionLinesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		pls, err := database.ListProductionLinesByFactory(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if pls == nil {
			pls = []*model.ProductionLine{}
		}
		writeJSON(w, http.StatusOK, pls)
	}
}

// GetProductionLineHandler fetches a single production line with its IO and machine groups.
// GET /api/production-lines/{line_id}
func GetProductionLineHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		plID, ok := parseUUIDParam(w, r, "line_id")
		if !ok {
			return
		}
		if err := requirePLOwner(r, w, database, plID, userID); err != nil {
			return
		}
		pl, err := database.GetProductionLine(r.Context(), plID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		mgs, err := database.ListMachineGroupsByPL(r.Context(), plID)
		if err != nil {
			errInternal(w, err)
			return
		}
		ios, err := database.ListPLIOByPL(r.Context(), plID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if mgs == nil {
			mgs = []*model.MachineGroup{}
		}
		if ios == nil {
			ios = []*model.PLIO{}
		}
		writeJSON(w, http.StatusOK, &model.ProductionLineDetail{
			ProductionLine: *pl,
			MachineGroups:  mgs,
			IO:             ios,
		})
	}
}

// UpdateProductionLineStatusHandler archives a production line.
// PATCH /api/production-lines/{line_id}/status
// Body: {"status": "archived"}
func UpdateProductionLineStatusHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		plID, ok := parseUUIDParam(w, r, "line_id")
		if !ok {
			return
		}
		if err := requirePLOwner(r, w, database, plID, userID); err != nil {
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if body.Status != "archived" {
			errBadRequest(w, `status must be "archived"`)
			return
		}
		if err := database.UpdateProductionLineStatus(r.Context(), plID, body.Status); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// MarkProductionLineBuiltHandler marks all planned machine groups in a line as built.
// PATCH /api/production-lines/{line_id}/mark-built
func MarkProductionLineBuiltHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		plID, ok := parseUUIDParam(w, r, "line_id")
		if !ok {
			return
		}
		if err := requirePLOwner(r, w, database, plID, userID); err != nil {
			return
		}
		n, err := database.MarkAllPlannedAsBuilt(r.Context(), plID)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"updated": n})
	}
}

// DeleteProductionLineHandler removes a production line and its child entities.
// DELETE /api/production-lines/{line_id}
func DeleteProductionLineHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		plID, ok := parseUUIDParam(w, r, "line_id")
		if !ok {
			return
		}
		if err := requirePLOwner(r, w, database, plID, userID); err != nil {
			return
		}
		if err := database.DeleteProductionLine(r.Context(), plID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// requirePLOwner verifies the requesting user owns the given production line.
func requirePLOwner(r *http.Request, w http.ResponseWriter, database *db.DB, plID, userID uuid.UUID) error {
	ownerID, err := database.ProductionLineOwnerUserID(r.Context(), plID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			errNotFound(w)
		} else {
			errInternal(w, err)
		}
		return err
	}
	if ownerID != userID {
		errForbidden(w)
		return errors.New("forbidden")
	}
	return nil
}
