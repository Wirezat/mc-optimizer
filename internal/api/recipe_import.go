package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/importer"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListValidRecipeTypesHandler returns all registered recipe type patterns.
// GET /api/valid-recipe-types
func ListValidRecipeTypesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vrts, err := database.ListValidRecipeTypes(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		if vrts == nil {
			vrts = []*model.ValidRecipeType{}
		}
		writeJSON(w, http.StatusOK, vrts)
	}
}

// CreateValidRecipeTypeHandler registers a new valid recipe type pattern.
// POST /api/valid-recipe-types
// Body: {"pattern":"mod:machine","is_regex":false,"target_mod_id":"...","target_machine_id":"..."}
func CreateValidRecipeTypeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Pattern         string  `json:"pattern"`
			IsRegex         bool    `json:"is_regex"`
			TargetModID     *string `json:"target_mod_id"`
			TargetMachineID *string `json:"target_machine_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if body.Pattern == "" {
			errBadRequest(w, "pattern is required")
			return
		}
		if (body.TargetModID == nil) != (body.TargetMachineID == nil) {
			errBadRequest(w, "target_mod_id and target_machine_id must be set together")
			return
		}

		vrt, err := database.CreateValidRecipeType(r.Context(), body.Pattern, body.IsRegex, body.TargetModID, body.TargetMachineID)
		if err != nil {
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "pattern already registered")
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, vrt)
	}
}

// DeleteValidRecipeTypeHandler removes a valid recipe type by ID.
// DELETE /api/valid-recipe-types/{vrt_id}
func DeleteValidRecipeTypeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "vrt_id")
		if !ok {
			return
		}
		if err := database.DeleteValidRecipeType(r.Context(), id); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ImportRecipesHandler accepts a JSON array of MI recipe objects and imports them.
// POST /api/import-recipes
// Body: array of MI recipe JSON objects (same format as datapack files)
// Response: {"imported": N, "skipped": N, "errors": [...]}
func ImportRecipesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var rawRecipes []json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&rawRecipes); err != nil {
			errBadRequest(w, "body must be a JSON array of recipe objects")
			return
		}

		recipes := make([]*importer.MIRecipe, 0, len(rawRecipes))
		var parseErrors []importer.Error

		for i, raw := range rawRecipes {
			rec, err := importer.ParseBytes(raw)
			if err != nil {
				parseErrors = append(parseErrors, importer.Error{
					Code:    CodeInvalidRecipeFormat,
					Message: fmt.Sprintf("recipe[%d]: %v", i, err),
				})
				continue
			}
			recipes = append(recipes, rec)
		}

		imp := importer.New(database)
		result, err := imp.Run(r.Context(), recipes)
		if err != nil {
			errInternal(w, err)
			return
		}

		result.Errors = append(parseErrors, result.Errors...)
		result.Skipped += len(parseErrors)

		writeJSON(w, http.StatusOK, result)
	}
}
