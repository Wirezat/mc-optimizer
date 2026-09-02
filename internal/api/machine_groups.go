package api

import (
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// UpdateMachineGroupStatusHandler sets the status of a single machine group.
func UpdateMachineGroupStatusHandler(database *db.DB) http.HandlerFunc {
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
			Status     string `json:"status"`
			BuiltCount *int   `json:"built_count"`
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

		w.WriteHeader(http.StatusNoContent)
	}
}

// SetGroupVariantHandler switches a machine group to another operating variant
// and recomputes how many machines that variant needs, preserving the recipe
// rate the group was solved for.
// PUT /api/machine-groups/{group_id}/variant
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
			solver.NewRational(mg.ExactCountNum, exactCountDen(mg)), from, to)
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

// groupVariants evaluates the variants available to a machine group, under the
// config frozen into the group when its production line was confirmed.
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

// errUnknownVariant and errInvalidVariant are the two ways a switch target
// can be rejected before the group's counts are ever recomputed.
var (
	errUnknownVariant = errors.New("unknown variant_id for this machine group")
	errInvalidVariant = errors.New("variant_id refers to an invalid variant")
)

// checkSwitchTarget rejects a switch target that findVariant did not resolve,
// or one that resolved but is not Valid.
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
