package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/service"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

// named* wrappers embed solver types with display names resolved at the API layer.
type namedItemRef struct {
	solver.ItemRef
	Name string `json:"name"`
}

type namedTagResolution struct {
	Chosen  namedItemRef
	Options []namedItemRef
}

type namedChainItem struct {
	solver.ChainItem
	Name string `json:"name"`
}

type namedIOEntry struct {
	solver.IOEntry
	Name string `json:"name"`
}

type namedMachineGroup struct {
	solver.MachineGroupDraft
	RecipeOutputName string `json:"recipe_output_name"`
	MachineName      string `json:"MachineName"`
}

func resolveTagResolutions(trs map[string]solver.TagResolution, names map[string]string) map[string]namedTagResolution {
	out := make(map[string]namedTagResolution, len(trs))
	for k, tr := range trs {
		opts := make([]namedItemRef, len(tr.Options))
		for i, o := range tr.Options {
			opts[i] = namedItemRef{ItemRef: o, Name: names[o.Key()]}
		}
		out[k] = namedTagResolution{
			Chosen:  namedItemRef{ItemRef: tr.Chosen, Name: names[tr.Chosen.Key()]},
			Options: opts,
		}
	}
	return out
}

// DiscoverHandler runs a BFS to enumerate all items in the production chain
// with their recipe options, without computing rates.
func DiscoverHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}

		var req struct {
			TargetItem      solver.ItemRef    `json:"TargetItem"`
			RecipeOverrides map[string]string `json:"RecipeOverrides"`
			TagOverrides    map[string]string `json:"TagOverrides"`
			StopPoints      map[string]bool   `json:"StopPoints"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.TargetItem.TagRef == "" && (req.TargetItem.ModID == "" || req.TargetItem.ItemID == "") {
			errBadRequest(w, "target_item requires mod_id+item_id or tag_ref")
			return
		}
		if req.RecipeOverrides == nil {
			req.RecipeOverrides = map[string]string{}
		}
		if req.TagOverrides == nil {
			req.TagOverrides = map[string]string{}
		}
		if req.StopPoints == nil {
			req.StopPoints = map[string]bool{}
		}

		s := solver.NewSolver(database, 0)
		result, err := s.Discover(r.Context(), req.TargetItem, req.StopPoints,
			solver.FactoryState{}, req.RecipeOverrides, req.TagOverrides)
		if err != nil {
			errInternal(w, err)
			return
		}

		var allRefs []solver.ItemRef
		for _, ci := range result.Items {
			allRefs = append(allRefs, ci.Item)
		}
		for _, tr := range result.TagResolutions {
			allRefs = append(allRefs, tr.Chosen)
			allRefs = append(allRefs, tr.Options...)
		}
		names, err := database.LookupItemNames(r.Context(), allRefs)
		if err != nil {
			errInternal(w, err)
			return
		}

		seen := map[string]bool{}
		var machineRefs []solver.MachineRef
		for _, ci := range result.Items {
			for _, opt := range ci.Options {
				k := opt.MachineMod + ":" + opt.MachineID
				if !seen[k] {
					seen[k] = true
					machineRefs = append(machineRefs, solver.MachineRef{ModID: opt.MachineMod, MachineID: opt.MachineID})
				}
			}
		}
		machineNames, err := database.LookupMachineNames(r.Context(), machineRefs)
		if err != nil {
			errInternal(w, err)
			return
		}

		namedItems := make([]namedChainItem, len(result.Items))
		for i, ci := range result.Items {
			namedItems[i] = namedChainItem{ChainItem: ci, Name: names[ci.Item.Key()]}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"Items":          namedItems,
			"TagResolutions": resolveTagResolutions(result.TagResolutions, names),
			"Names":          names,
			"MachineNames":   machineNames,
		})
	}
}

// SolveHandler runs the solver for a factory and stores a solver_draft.
func SolveHandler(database *db.DB, svc *service.PLService) http.HandlerFunc {
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
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.TargetItem.TagRef == "" && (req.TargetItem.ModID == "" || req.TargetItem.ItemID == "") {
			errBadRequest(w, "target_item requires mod_id+item_id or tag_ref")
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

		out, err := svc.Solve(r.Context(), factoryID, userID, req)
		if err != nil {
			var cycleErr *solver.ErrCycleBreakNeeded
			if errors.As(err, &cycleErr) {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":       "CYCLE_BREAK_NEEDED",
					"cycle_nodes": cycleErr.CycleNodes,
				})
				return
			}
			if errors.Is(err, solver.ErrNoSolution) || errors.Is(err, solver.ErrUnderDetermined) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
					"error":   "NO_SOLUTION",
					"message": "No valid production chain found. Check that the target item has recipes and that stop points don't cut all paths.",
				})
				return
			}
			errInternal(w, err)
			return
		}
		var allRefs []solver.ItemRef
		for _, mg := range out.Result.MachineGroups {
			allRefs = append(allRefs, mg.RecipeOutput)
		}
		for _, e := range out.Result.IOProfile.Inputs {
			allRefs = append(allRefs, e.Item)
		}
		for _, e := range out.Result.IOProfile.Outputs {
			allRefs = append(allRefs, e.Item)
		}
		for _, tr := range out.Result.TagResolutions {
			allRefs = append(allRefs, tr.Chosen)
			allRefs = append(allRefs, tr.Options...)
		}
		names, err := database.LookupItemNames(r.Context(), allRefs)
		if err != nil {
			errInternal(w, err)
			return
		}

		machineRefs := make([]solver.MachineRef, 0, len(out.Result.MachineGroups))
		seen := make(map[string]bool)
		for _, mg := range out.Result.MachineGroups {
			k := mg.MachineMod + ":" + mg.MachineID
			if !seen[k] {
				seen[k] = true
				machineRefs = append(machineRefs, solver.MachineRef{ModID: mg.MachineMod, MachineID: mg.MachineID})
			}
		}
		machineNames, err := database.LookupMachineNames(r.Context(), machineRefs)
		if err != nil {
			errInternal(w, err)
			return
		}

		namedGroups := make([]namedMachineGroup, len(out.Result.MachineGroups))
		for i, mg := range out.Result.MachineGroups {
			namedGroups[i] = namedMachineGroup{
				MachineGroupDraft: mg,
				RecipeOutputName:  names[mg.RecipeOutput.Key()],
				MachineName:       machineNames[mg.MachineMod+":"+mg.MachineID],
			}
		}
		namedInputs := make([]namedIOEntry, len(out.Result.IOProfile.Inputs))
		for i, e := range out.Result.IOProfile.Inputs {
			namedInputs[i] = namedIOEntry{IOEntry: e, Name: names[e.Item.Key()]}
		}
		namedOutputs := make([]namedIOEntry, len(out.Result.IOProfile.Outputs))
		for i, e := range out.Result.IOProfile.Outputs {
			namedOutputs[i] = namedIOEntry{IOEntry: e, Name: names[e.Item.Key()]}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"draft_id":   out.DraftID,
			"expires_at": out.ExpiresAt,
			"Names":      names,
			"result": map[string]any{
				"MachineGroups": namedGroups,
				"IOProfile": map[string]any{
					"Inputs":  namedInputs,
					"Outputs": namedOutputs,
				},
				"ActualRate":     out.Result.ActualRate,
				"HadCycles":      out.Result.HadCycles,
				"ModeUsed":       out.Result.ModeUsed,
				"Warnings":       out.Result.Warnings,
				"TagResolutions": resolveTagResolutions(out.Result.TagResolutions, names),
			},
		})
	}
}

// ConfirmProductionLineHandler promotes a solver_draft to a live production line.
func ConfirmProductionLineHandler(database *db.DB, svc *service.PLService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}

		var body struct {
			DraftID string `json:"draft_id"`
			Name    string `json:"name"`
		}
		if !decodeJSON(w, r, &body) {
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

		detail, err := svc.Confirm(r.Context(), factoryID, service.ConfirmInput{
			DraftID: draftID,
			Name:    body.Name,
		})
		if err != nil {
			switch {
			case errors.Is(err, service.ErrDraftNotFound):
				errNotFound(w)
			case errors.Is(err, service.ErrWrongFactory):
				errForbidden(w)
			default:
				errInternal(w, err)
			}
			return
		}
		writeJSON(w, http.StatusCreated, detail)
	}
}

// ListProductionLinesHandler returns all production lines for a factory.
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
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Status != "archived" && body.Status != "active" {
			errBadRequest(w, `status must be "archived" or "active"`)
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

// ResolveProductionLineHandler re-solves an existing line (e.g. for more output) using its
// stored solver request with the given overrides, replacing its machine groups and IO.
//        "upgrade_mode":"auto"|"fixed"|"off", "upgrade_tier":"<uuid>", "upgrade_count":N}
func ResolveProductionLineHandler(database *db.DB, svc *service.PLService) http.HandlerFunc {
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
			TargetRate   *solver.Rational `json:"target_rate"`
			TimeUnit     string           `json:"time_unit"`
			UpgradeMode  string           `json:"upgrade_mode"`
			UpgradeTier  string           `json:"upgrade_tier"`
			UpgradeCount int              `json:"upgrade_count"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		detail, err := svc.Resolve(r.Context(), plID, service.ResolveInput{
			TargetRate:   body.TargetRate,
			TimeUnit:     body.TimeUnit,
			UpgradeMode:  body.UpgradeMode,
			UpgradeTier:  body.UpgradeTier,
			UpgradeCount: body.UpgradeCount,
		})
		if err != nil {
			var cycleErr *solver.ErrCycleBreakNeeded
			if errors.As(err, &cycleErr) {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":       "CYCLE_BREAK_NEEDED",
					"cycle_nodes": cycleErr.CycleNodes,
				})
				return
			}
			if errors.Is(err, service.ErrNoStoredRequest) {
				errBadRequest(w, "this line cannot be re-solved (no stored request)")
				return
			}
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

// MarkProductionLineBuiltHandler marks all planned machine groups in a line as built.
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

// RenamePLHandler updates the name of a production line.
func RenamePLHandler(database *db.DB) http.HandlerFunc {
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
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			errBadRequest(w, "name is required")
			return
		}
		if err := database.RenameProductionLine(r.Context(), plID, body.Name); err != nil {
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
