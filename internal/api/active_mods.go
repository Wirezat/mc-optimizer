package api

import (
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// GetActiveModsHandler returns the set of mod IDs active for a save.
func GetActiveModsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		saveID, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if _, err := database.GetSave(r.Context(), saveID, userIDFromContext(r.Context())); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		mods, err := database.GetActiveMods(r.Context(), saveID)
		if err != nil {
			errInternal(w, err)
			return
		}
		ids := make([]string, 0, len(mods))
		for id := range mods {
			ids = append(ids, id)
		}
		writeJSON(w, http.StatusOK, map[string]any{"mod_ids": ids})
	}
}

// CheckActiveModDependentsHandler returns the production lines that would be affected if
// the given mod IDs were deactivated for this save — the client calls this with the set of
// mods being newly unchecked, before actually saving, to decide whether to show a
// confirmation prompt.
func CheckActiveModDependentsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		saveID, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if _, err := database.GetSave(r.Context(), saveID, userIDFromContext(r.Context())); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		var req struct {
			ModIDs []string `json:"mod_ids"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		lines, err := database.GetDependentProductionLines(r.Context(), saveID, req.ModIDs)
		if err != nil {
			errInternal(w, err)
			return
		}
		if lines == nil {
			lines = []*model.DependentProductionLine{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"production_lines": lines})
	}
}

// SetActiveModsHandler replaces the full active-mod set for a save.
func SetActiveModsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		saveID, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if _, err := database.GetSave(r.Context(), saveID, userIDFromContext(r.Context())); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		var req struct {
			ModIDs []string `json:"mod_ids"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := database.SetActiveMods(r.Context(), saveID, req.ModIDs); err != nil {
			errInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
