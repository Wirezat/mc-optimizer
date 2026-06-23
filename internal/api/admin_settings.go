package api

import (
	"encoding/json"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
)

// GetAdminSettingsHandler handles GET /api/admin/settings.
// Returns current admin settings visible to admins.
func GetAdminSettingsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enabled, err := database.RegistrationEnabled(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"registration_enabled": enabled,
		})
	}
}

// UpdateAdminSettingsHandler handles PATCH /api/admin/settings.
// Allows admins to update individual settings.
func UpdateAdminSettingsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RegistrationEnabled *bool `json:"registration_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if body.RegistrationEnabled != nil {
			val := "false"
			if *body.RegistrationEnabled {
				val = "true"
			}
			if err := database.SetSetting(r.Context(), "registration_enabled", val); err != nil {
				errInternal(w, err)
				return
			}
		}
		enabled, err := database.RegistrationEnabled(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"registration_enabled": enabled,
		})
	}
}
