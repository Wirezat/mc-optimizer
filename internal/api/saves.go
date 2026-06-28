package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListSavesHandler returns all saves for the authenticated user.
func ListSavesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		saves, err := database.ListSavesByUser(r.Context(), userIDFromContext(r.Context()))
		if err != nil {
			errInternal(w, err)
			return
		}
		if saves == nil {
			saves = []*model.Save{}
		}
		writeJSON(w, http.StatusOK, saves)
	}
}

// GetSaveHandler fetches a single save by ID, enforcing ownership.
func GetSaveHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		s, err := database.GetSave(r.Context(), id, userIDFromContext(r.Context()))
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s)
	}
}

// CreateSaveHandler creates a named save for the authenticated user.
func CreateSaveHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		s, err := database.CreateSave(r.Context(), userIDFromContext(r.Context()), body.Name)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, s)
	}
}

// DeleteSaveHandler deletes a save by ID, enforcing ownership.
func DeleteSaveHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if err := database.DeleteSave(r.Context(), id, userIDFromContext(r.Context())); err != nil {
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
