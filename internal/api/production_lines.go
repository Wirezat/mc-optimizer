package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/service"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

// named* wrappers embed solver types with display names resolved at the API layer.
type namedItemRef struct {
	solver.ResourceRef
	Name string `json:"name"`
}

type namedTagResolution struct {
	Chosen  namedItemRef   `json:"chosen"`
	Options []namedItemRef `json:"options"`
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
	MachineName      string `json:"machine_name"`
}

func resolveTagResolutions(trs map[string]solver.TagResolution, names map[string]string) map[string]namedTagResolution {
	out := make(map[string]namedTagResolution, len(trs))
	for k, tr := range trs {
		opts := make([]namedItemRef, len(tr.Options))
		for i, o := range tr.Options {
			opts[i] = namedItemRef{ResourceRef: o, Name: names[o.Key()]}
		}
		out[k] = namedTagResolution{
			Chosen:  namedItemRef{ResourceRef: tr.Chosen, Name: names[tr.Chosen.Key()]},
			Options: opts,
		}
	}
	return out
}

// discoverRequest is the shared wire format for both the real (factory-scoped) and demo
// discover endpoints.
type discoverRequest struct {
	TargetItem      solver.ResourceRef `json:"target"`
	RecipeOverrides map[string]string  `json:"recipe_overrides"`
	TagOverrides    map[string]string  `json:"tag_overrides"`
	StopPoints      map[string]bool    `json:"stop_points"`
}

func decodeDiscoverRequest(w http.ResponseWriter, r *http.Request) (discoverRequest, bool) {
	var req discoverRequest
	if !decodeJSON(w, r, &req) {
		return req, false
	}
	if req.TargetItem.TagRef == "" && (req.TargetItem.ModID == "" || req.TargetItem.ID == "") {
		errBadRequest(w, "target requires mod_id+id or tag_ref")
		return req, false
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
	return req, true
}

// respondDiscover resolves display names for a DiscoverResult and writes the JSON response.
func respondDiscover(w http.ResponseWriter, r *http.Request, database *db.DB, result solver.DiscoverResult) {
	var allRefs []solver.ResourceRef
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
	machinePluginMods, err := database.LookupMachinePluginMods(r.Context(), machineRefs)
	if err != nil {
		errInternal(w, err)
		return
	}

	namedItems := make([]namedChainItem, len(result.Items))
	for i, ci := range result.Items {
		namedItems[i] = namedChainItem{ChainItem: ci, Name: names[ci.Item.Key()]}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":               namedItems,
		"tag_resolutions":     resolveTagResolutions(result.TagResolutions, names),
		"names":               names,
		"machine_names":       machineNames,
		"machine_plugin_mods": machinePluginMods,
	})
}

// DiscoverHandler runs a BFS to enumerate all items in the production chain with their
// recipe options, without computing rates.
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

		activeMods, ok := loadActiveModsForFactory(w, r, database, factoryID)
		if !ok {
			return
		}

		req, ok := decodeDiscoverRequest(w, r)
		if !ok {
			return
		}

		s := solver.NewSolver(database, 0)
		s.ActiveMods = activeMods
		result, err := s.Discover(r.Context(), req.TargetItem, req.StopPoints,
			solver.FactoryState{}, req.RecipeOverrides, req.TagOverrides)
		if err != nil {
			errInternal(w, err)
			return
		}
		respondDiscover(w, r, database, result)
	}
}

func loadActiveModsForFactory(w http.ResponseWriter, r *http.Request, database *db.DB, factoryID uuid.UUID) (map[string]bool, bool) {
	factory, err := database.GetFactory(r.Context(), factoryID)
	if err != nil {
		errInternal(w, err)
		return nil, false
	}
	activeMods, err := database.GetActiveMods(r.Context(), factory.SaveID)
	if err != nil {
		errInternal(w, err)
		return nil, false
	}
	return activeMods, true
}

// DemoDiscoverHandler is the factory-less counterpart to DiscoverHandler, used by the
// /demo/solve page.
func DemoDiscoverHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeDiscoverRequest(w, r)
		if !ok {
			return
		}

		s := solver.NewSolver(database, 0)
		result, err := s.Discover(r.Context(), req.TargetItem, req.StopPoints,
			solver.FactoryState{}, req.RecipeOverrides, req.TagOverrides)
		if err != nil {
			errInternal(w, err)
			return
		}
		respondDiscover(w, r, database, result)
	}
}

// SolveHandler runs the solver for a factory and stores a solver_draft.
func decodeSolveRequest(w http.ResponseWriter, r *http.Request) (solver.SolveRequest, bool) {
	var req solver.SolveRequest
	if !decodeJSON(w, r, &req) {
		return req, false
	}
	if req.TargetItem.TagRef == "" && (req.TargetItem.ModID == "" || req.TargetItem.ID == "") {
		errBadRequest(w, "target requires mod_id+id or tag_ref")
		return req, false
	}
	if req.TimeUnit == "" {
		errBadRequest(w, "time_unit is required")
		return req, false
	}
	if req.TargetRate.Den == 0 {
		errBadRequest(w, "target_rate is required")
		return req, false
	}
	return req, true
}

// writeSolveError maps solver errors to their HTTP responses. Returns true if it wrote a
// response (caller should stop), false if err was nil.
func writeSolveError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var cycleErr *solver.ErrCycleBreakNeeded
	if errors.As(err, &cycleErr) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       "CYCLE_BREAK_NEEDED",
			"cycle_nodes": cycleErr.CycleNodes,
		})
		return true
	}
	if errors.Is(err, solver.ErrNoSolution) || errors.Is(err, solver.ErrUnderDetermined) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error":   "NO_SOLUTION",
			"message": "No valid production chain found. Check that the target item has recipes and that stop points don't cut all paths.",
		})
		return true
	}
	var negErr *solver.ErrNegativeRate
	if errors.As(err, &negErr) {
		// A chain that can only balance by running a recipe backwards is a property of the
		// catalog, not a server fault. The recipe id goes to the log, not to the unauthenticated
		// /api/demo/solve response.
		GoLog.Warnf("solve: %v", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "NEGATIVE_RATE",
		})
		return true
	}
	if errors.Is(err, solver.ErrRateOverflow) || errors.Is(err, solver.ErrRateDomain) {
		// A structured code, not a message: the client picks its own translated text
		// (solve.error.rate_overflow), same as CYCLE_BREAK_NEEDED.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "RATE_OVERFLOW",
		})
		return true
	}
	errInternal(w, err)
	return true
}

// respondSolve resolves display names for a SolveResult and writes the JSON response.
func respondSolve(w http.ResponseWriter, r *http.Request, database *db.DB, result solver.SolveResult, draftID *uuid.UUID, expiresAt *time.Time) {
	var allRefs []solver.ResourceRef
	for _, mg := range result.MachineGroups {
		allRefs = append(allRefs, mg.RecipeOutput)
	}
	for _, e := range result.IOProfile.Inputs {
		allRefs = append(allRefs, e.Item)
	}
	for _, e := range result.IOProfile.Outputs {
		allRefs = append(allRefs, e.Item)
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

	machineRefs := make([]solver.MachineRef, 0, len(result.MachineGroups))
	seen := make(map[string]bool)
	for _, mg := range result.MachineGroups {
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

	namedGroups := make([]namedMachineGroup, len(result.MachineGroups))
	for i, mg := range result.MachineGroups {
		namedGroups[i] = namedMachineGroup{
			MachineGroupDraft: mg,
			RecipeOutputName:  names[mg.RecipeOutput.Key()],
			MachineName:       machineNames[mg.MachineMod+":"+mg.MachineID],
		}
	}
	namedInputs := make([]namedIOEntry, len(result.IOProfile.Inputs))
	for i, e := range result.IOProfile.Inputs {
		namedInputs[i] = namedIOEntry{IOEntry: e, Name: names[e.Item.Key()]}
	}
	namedOutputs := make([]namedIOEntry, len(result.IOProfile.Outputs))
	for i, e := range result.IOProfile.Outputs {
		namedOutputs[i] = namedIOEntry{IOEntry: e, Name: names[e.Item.Key()]}
	}
	resp := map[string]any{
		"names": names,
		"result": map[string]any{
			"machine_groups": namedGroups,
			"io_profile": map[string]any{
				"inputs":  namedInputs,
				"outputs": namedOutputs,
			},
			"actual_rate":     result.ActualRate,
			"had_cycles":      result.HadCycles,
			"mode_used":       result.ModeUsed,
			"warnings":        result.Warnings,
			"tag_resolutions": resolveTagResolutions(result.TagResolutions, names),
		},
	}
	if draftID != nil {
		resp["draft_id"] = *draftID
	}
	if expiresAt != nil {
		resp["expires_at"] = *expiresAt
	}
	writeJSON(w, http.StatusOK, resp)
}

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

		req, ok := decodeSolveRequest(w, r)
		if !ok {
			return
		}

		out, err := svc.Solve(r.Context(), factoryID, userID, req)
		if writeSolveError(w, err) {
			return
		}
		respondSolve(w, r, database, out.Result, &out.DraftID, &out.ExpiresAt)
	}
}

// DemoSolveHandler is the factory-less counterpart to SolveHandler, used by the /demo/solve
// page.
func DemoSolveHandler(database *db.DB, autoScaleMax int64, variants solver.VariantSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeSolveRequest(w, r)
		if !ok {
			return
		}
		req.FactoryState = solver.FactoryState{}
		req.ModConfigs = nil

		sv := solver.NewSolver(database, autoScaleMax)
		sv.VariantSource = variants
		result, err := sv.Solve(r.Context(), req)
		if writeSolveError(w, err) {
			return
		}
		respondSolve(w, r, database, result, nil, nil)
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
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		draftID, err := uuid.Parse(strings.TrimSpace(body.DraftID))
		if err != nil {
			errBadRequest(w, "draft_id must be a valid UUID")
			return
		}
		detail, err := svc.Confirm(r.Context(), factoryID, service.ConfirmInput{DraftID: draftID})
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
func ListProductionLinesHandler(database *db.DB, variants solver.VariantSource) http.HandlerFunc {
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
		for _, pl := range pls {
			mgs, err := database.ListMachineGroupsByPL(r.Context(), pl.ID)
			if err != nil {
				errInternal(w, err)
				return
			}
			frac := db.EstimateCurrentRateFraction(mgs, enrichGroupVariants(r, database, variants, mgs))
			if pl.RateDen > 0 {
				pl.CurrentRate = frac * float64(pl.RateNum) / float64(pl.RateDen)
			}
			pl.Costs = aggregateCosts(groupOperatingCosts(r, database, variants, mgs))
			pl.Machines = aggregateMachines(mgs)
		}
		writeJSON(w, http.StatusOK, pls)
	}
}

// aggregateMachines folds a line's machine groups into one entry per machine kind, busiest
// first — a line built from three bronze macerators in two groups runs one kind of machine,
// six of them, and the list says so.
func aggregateMachines(mgs []*model.MachineGroup) []model.MachineUse {
	idx := make(map[string]int, len(mgs))
	out := make([]model.MachineUse, 0, len(mgs))
	for _, mg := range mgs {
		key := mg.MachineModID + ":" + mg.MachineID
		if i, ok := idx[key]; ok {
			out[i].Count += mg.Count
			continue
		}
		idx[key] = len(out)
		out = append(out, model.MachineUse{
			MachineModID: mg.MachineModID,
			MachineID:    mg.MachineID,
			Count:        mg.Count,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].MachineModID+":"+out[i].MachineID < out[j].MachineModID+":"+out[j].MachineID
	})
	return out
}

// groupOperatingCosts resolves each machine group's chosen operating variant and scales its
// per-machine costs by the group's machine count.
func groupOperatingCosts(r *http.Request, database *db.DB, variants solver.VariantSource, mgs []*model.MachineGroup) [][]plugins.Cost {
	out := make([][]plugins.Cost, 0, len(mgs))
	for _, mg := range mgs {
		vs, err := groupVariants(r, database, variants, mg)
		if err != nil {
			GoLog.Warnf("production lines: resolve variants for group %s: %v", mg.ID, err)
			continue
		}
		v, ok := findVariant(vs, mg.VariantID)
		if !ok {
			continue
		}
		out = append(out, scaleCosts(v.Costs, mg.Count))
	}
	return out
}

// GetProductionLineHandler fetches a single production line with its IO and machine groups,
// each group carrying its target and built variant.
func GetProductionLineHandler(database *db.DB, variants solver.VariantSource) http.HandlerFunc {
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
		frac := db.EstimateCurrentRateFraction(mgs, enrichGroupVariants(r, database, variants, mgs))
		if pl.RateDen > 0 {
			pl.CurrentRate = frac * float64(pl.RateNum) / float64(pl.RateDen)
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

// ScaleProductionLineHandler handles POST /api/production-lines/{line_id}/scale: the manual
// override that resizes a line to a multiple of what the solver produced.
func ScaleProductionLineHandler(database *db.DB, svc *service.PLService) http.HandlerFunc {
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
			Factor solver.Rational `json:"factor"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Factor.Den == 0 || !body.Factor.IsPositive() {
			errBadRequest(w, "factor must be positive")
			return
		}

		detail, err := svc.Scale(r.Context(), plID, body.Factor)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrBuiltCountExceeded):
				errConflict(w, err.Error())
			case errors.Is(err, db.ErrNotFound):
				errNotFound(w)
			default:
				errInternal(w, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
