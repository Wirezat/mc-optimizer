package api

import (
	"errors"
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

// UpdateMachineGroupStatusHandler sets the status, build state and built variant of a
// single machine group.
func UpdateMachineGroupStatusHandler(database *db.DB, variants solver.VariantSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		groupID, ok := parseUUIDParam(w, r, "group_id")
		if !ok {
			return
		}
		ownerID, err := database.MachineGroupOwnerUserID(r.Context(), groupID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		if ownerID != userID {
			errForbidden(w)
			return
		}
		var body struct {
			Status           string `json:"status"`
			BuiltCount       *int   `json:"built_count"`
			CurrentVariantID string `json:"current_variant_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}

		if body.Status != "" {
			switch body.Status {
			case "planned", "built", "archived":
			default:
				errBadRequest(w, `status must be one of: planned, built, archived`)
				return
			}
			if err := database.UpdateMachineGroupStatus(r.Context(), groupID, body.Status); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					errNotFound(w)
				} else {
					errInternal(w, err)
				}
				return
			}
		}

		if body.BuiltCount != nil {
			mg, err := database.GetMachineGroup(r.Context(), groupID)
			if err != nil {
				if errors.Is(err, db.ErrNotFound) {
					errNotFound(w)
				} else {
					errInternal(w, err)
				}
				return
			}

			builtCount := *body.BuiltCount
			if builtCount < 0 || builtCount > mg.Count {
				errBadRequest(w, "built_count must be between 0 and count")
				return
			}

			if err := database.UpdateMachineGroupBuildState(r.Context(), groupID, builtCount); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					errNotFound(w)
				} else {
					errInternal(w, err)
				}
				return
			}
		}

		if body.CurrentVariantID != "" {
			mg, err := database.GetMachineGroup(r.Context(), groupID)
			if err != nil {
				writeGroupError(w, err)
				return
			}
			vs, err := groupVariants(r, database, variants, mg)
			if err != nil {
				writeGroupError(w, err)
				return
			}
			if _, ok := findVariant(vs, body.CurrentVariantID); !ok {
				errBadRequest(w, errUnknownVariant.Error())
				return
			}
			if err := database.UpdateMachineGroupCurrentVariant(r.Context(), groupID, body.CurrentVariantID); err != nil {
				writeGroupError(w, err)
				return
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// variantView flattens a variant for a client, resolving item display names through names
// (keyed by item ref; a miss keeps the ref).
func variantView(v plugins.Variant, names map[string]string) model.VariantView {
	items := make([]model.VariantItem, 0, len(v.Items))
	for _, it := range v.Items {
		name := names[it.Ref]
		if name == "" {
			name = it.Ref
		}
		items = append(items, model.VariantItem{Ref: it.Ref, Name: name, Count: it.Count})
	}
	return model.VariantView{ID: v.ID, Label: v.Label, Items: items}
}

// baseVariant is the runnable variant with nothing installed.
func baseVariant(vs []plugins.Variant) (plugins.Variant, bool) {
	for _, v := range vs {
		if len(v.Items) == 0 && v.Valid {
			return v, true
		}
	}
	return plugins.Variant{}, false
}

// enrichGroupVariants fills each group's Variant, CurrentVariant and VariantOptions from
// its plugin and returns the built variant's speed relative to the target's, keyed by group
// id, for the current-rate estimate.
func enrichGroupVariants(r *http.Request, database *db.DB, variants solver.VariantSource, mgs []*model.MachineGroup) map[uuid.UUID]float64 {
	speed := make(map[uuid.UUID]float64, len(mgs))
	perGroup := make([][]plugins.Variant, len(mgs))
	var refs []resource.Ref
	for i, mg := range mgs {
		vs, err := groupVariants(r, database, variants, mg)
		if err != nil {
			GoLog.Warnf("production lines: resolve variants for group %s: %v", mg.ID, err)
			continue
		}
		perGroup[i] = vs
		for _, v := range vs {
			for _, it := range v.Items {
				refs = append(refs, resource.RefFromKey(it.Ref))
			}
		}
	}
	names, err := database.LookupItemNames(r.Context(), refs)
	if err != nil {
		GoLog.Warnf("production lines: lookup variant item names: %v", err)
		names = map[string]string{}
	}
	for i, mg := range mgs {
		vs := perGroup[i]
		if vs == nil {
			continue
		}
		target, ok := findVariant(vs, mg.VariantID)
		if !ok {
			continue
		}
		current, ok := findVariant(vs, mg.CurrentVariantID)
		if !ok {
			if current, ok = baseVariant(vs); !ok {
				current = target
			}
		}
		tv, cv := variantView(target, names), variantView(current, names)
		mg.Variant, mg.CurrentVariant = &tv, &cv
		mg.VariantOptions = make([]model.VariantView, 0, len(vs))
		for _, v := range vs {
			if v.Valid {
				mg.VariantOptions = append(mg.VariantOptions, variantView(v, names))
			}
		}
		if target.Rate.Num > 0 && target.Rate.Den > 0 && current.Rate.Num > 0 && current.Rate.Den > 0 {
			speed[mg.ID] = (float64(current.Rate.Num) / float64(current.Rate.Den)) /
				(float64(target.Rate.Num) / float64(target.Rate.Den))
		}
	}
	return speed
}

// groupVariants evaluates the variants available to a machine group, under the config
// frozen into the group when its production line was confirmed.
func groupVariants(r *http.Request, database *db.DB, variants solver.VariantSource, mg *model.MachineGroup) ([]plugins.Variant, error) {
	machine, err := database.GetMachineType(r.Context(), mg.MachineModID, mg.MachineID)
	if err != nil {
		return nil, err
	}
	recipe, err := database.GetRecipe(r.Context(), mg.RecipeID.String())
	if err != nil {
		return nil, err
	}
	return variants.Variants(r.Context(), machine, recipe, mg.ModConfig)
}

// findVariant returns the variant with the given id.
func findVariant(vs []plugins.Variant, id string) (plugins.Variant, bool) {
	for _, v := range vs {
		if v.ID == id {
			return v, true
		}
	}
	return plugins.Variant{}, false
}

// errUnknownVariant and errInvalidVariant are the two ways a switch target can be rejected.
var (
	errUnknownVariant = errors.New("unknown variant_id for this machine group")
	errInvalidVariant = errors.New("variant_id refers to an invalid variant")
)

// checkSwitchTarget rejects a target findVariant did not resolve, or one that is not Valid.
func checkSwitchTarget(to plugins.Variant, hasTo bool) error {
	if !hasTo {
		return errUnknownVariant
	}
	if !to.Valid {
		return errInvalidVariant
	}
	return nil
}

// exactCountDen guards against a zero denominator from an older row.
func exactCountDen(mg *model.MachineGroup) int64 {
	if mg.ExactCountDen == 0 {
		return 1
	}
	return mg.ExactCountDen
}

// writeGroupError maps a machine group lookup failure to its HTTP response.
func writeGroupError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNotFound) {
		errNotFound(w)
		return
	}
	errInternal(w, err)
}

// SetGroupVariantHandler switches a machine group to another operating variant and
// recomputes its counts. Input: { "variant_id": "<id>" }. Output: the updated group.
func SetGroupVariantHandler(database *db.DB, variants solver.VariantSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, ok := parseUUIDParam(w, r, "group_id")
		if !ok {
			return
		}
		ownerID, _, err := database.MachineGroupScope(r.Context(), groupID)
		if err != nil {
			writeGroupError(w, err)
			return
		}
		if ownerID != userIDFromContext(r.Context()) {
			errForbidden(w)
			return
		}
		var body struct {
			VariantID string `json:"variant_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.VariantID == "" {
			errBadRequest(w, "variant_id is required")
			return
		}

		mg, err := database.GetMachineGroup(r.Context(), groupID)
		if err != nil {
			writeGroupError(w, err)
			return
		}
		vs, err := groupVariants(r, database, variants, mg)
		if err != nil {
			writeGroupError(w, err)
			return
		}

		to, hasTo := findVariant(vs, body.VariantID)
		if err := checkSwitchTarget(to, hasTo); err != nil {
			errBadRequest(w, err.Error())
			return
		}
		from, hasFrom := findVariant(vs, mg.VariantID)
		if !hasFrom {
			// The group's recipe rate cannot be reconstructed without the
			// variant its stored counts were computed under.
			errConflict(w, "the group's current variant no longer exists; re-solve the production line")
			return
		}

		count, exact, err := solver.RecountForVariant(
			resource.NewRational(mg.ExactCountNum, exactCountDen(mg)), from, to)
		if err != nil {
			errBadRequest(w, err.Error())
			return
		}
		if err := database.UpdateMachineGroupVariant(r.Context(), groupID, to.ID, int(count), exact.Num, exact.Den); err != nil {
			writeGroupError(w, err)
			return
		}

		mg.VariantID = to.ID
		mg.Count = int(count)
		mg.ExactCountNum, mg.ExactCountDen = exact.Num, exact.Den
		mg.BuiltCount = min(mg.BuiltCount, mg.Count)
		mg.Costs = to.Costs
		writeJSON(w, http.StatusOK, mg)
	}
}
