package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/google/uuid"
)

// ListFactoriesHandler returns all factories belonging to a save.
func ListFactoriesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		saveID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := requireSaveOwner(r, w, database, saveID, userID); err != nil {
			return
		}
		factories, err := database.ListFactoriesBySave(r.Context(), saveID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if factories == nil {
			factories = []*db.Factory{}
		}
		writeJSON(w, http.StatusOK, factories)
	}
}

// CreateFactoryHandler creates a named factory under the given save.
func CreateFactoryHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		saveID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := requireSaveOwner(r, w, database, saveID, userID); err != nil {
			return
		}
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
		f, err := database.CreateFactory(r.Context(), saveID, body.Name)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, f)
	}
}

// GetFactoryHandler fetches a single factory by ID, enforcing ownership.
func GetFactoryHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		f, err := database.GetFactory(r.Context(), factoryID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, f)
	}
}

// DeleteFactoryHandler removes a factory by ID, enforcing ownership.
func DeleteFactoryHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		if err := database.DeleteFactory(r.Context(), factoryID); err != nil {
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

// requireSaveOwner verifies the requesting user owns the given save.
func requireSaveOwner(r *http.Request, w http.ResponseWriter, database *db.DB, saveID, userID uuid.UUID) error {
	if _, err := database.GetSave(r.Context(), saveID, userID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			errNotFound(w)
		} else {
			errInternal(w, err)
		}
		return err
	}
	return nil
}

// requireFactoryOwner verifies the requesting user owns the given factory.
func requireFactoryOwner(r *http.Request, w http.ResponseWriter, database *db.DB, factoryID, userID uuid.UUID) error {
	ownerID, err := database.FactoryOwnerUserID(r.Context(), factoryID)
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
