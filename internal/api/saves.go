package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
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

// CreateSaveHandler creates a named save for the authenticated user.
func CreateSaveHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
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
		id, ok := parseUUIDParam(w, r, "id")
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

// parseUUIDParam parses a named path param as UUID; writes 400 and returns false on failure.
func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		errBadRequest(w, name+" must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}
