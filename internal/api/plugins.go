package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
)

// maxModConfigBytes bounds a mod config body.
const maxModConfigBytes = 1 << 20

// PluginAssetHandler serves a mod's plugin source to the browser, which evaluates it with
// the same one-file convention goja uses server-side. GET /plugin-assets/{mod_id}/plugin.js
func PluginAssetHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := database.GetModPlugin(r.Context(), r.PathValue("mod_id"))
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		// A re-import replaces the source at the same URL, and a stale wizard would write a
		// config the current plugin cannot read.
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := io.WriteString(w, p.Source); err != nil {
			return
		}
	}
}

// GetSaveModConfigDefaultsHandler returns every mod's seed config for a save, keyed by mod
// id. GET /api/saves/{save_id}/mod-config-defaults
func GetSaveModConfigDefaultsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		saveID, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if err := requireSaveOwner(r, w, database, saveID, userIDFromContext(r.Context())); err != nil {
			return
		}
		defaults, err := database.GetSaveModConfigDefaults(r.Context(), saveID)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, defaults)
	}
}

// SetSaveModConfigDefaultHandler stores one mod's seed config for a save. PUT
// /api/saves/{save_id}/mod-config-defaults/{mod_id}
func SetSaveModConfigDefaultHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		saveID, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if err := requireSaveOwner(r, w, database, saveID, userIDFromContext(r.Context())); err != nil {
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxModConfigBytes))
		if err != nil {
			errBadRequest(w, "config body is too large or could not be read")
			return
		}
		if !json.Valid(body) {
			errBadRequest(w, "config must be valid JSON")
			return
		}
		if err := database.SetSaveModConfigDefault(r.Context(), saveID, r.PathValue("mod_id"), body); err != nil {
			errInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
