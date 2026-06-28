package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/fracidx"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
)

// ListPLGroupsHandler returns all PL groups for a factory.
func ListPLGroupsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		groups, err := database.ListPLGroupsByFactory(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if groups == nil {
			groups = []*model.PLGroup{}
		}
		writeJSON(w, http.StatusOK, groups)
	}
}

// CreatePLGroupHandler creates a new PL group inside a factory.
func CreatePLGroupHandler(database *db.DB) http.HandlerFunc {
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
		last, err := database.GetLastGroupPosition(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		g, err := database.CreatePLGroup(r.Context(), factoryID, body.Name, fracidx.Between(last, ""))
		if err != nil {
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "a group with this name already exists")
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, g)
	}
}

// RenamePLGroupHandler updates the name of a PL group.
func RenamePLGroupHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		groupID, ok := parseUUIDParam(w, r, "group_id")
		if !ok {
			return
		}
		if err := requirePLGroupOwner(r, w, database, groupID, userID); err != nil {
			return
		}
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
		if err := database.RenamePLGroup(r.Context(), groupID, body.Name); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "a group with this name already exists")
				return
			}
			errInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeletePLGroupHandler removes a PL group (PLs in it become ungrouped).
func DeletePLGroupHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		groupID, ok := parseUUIDParam(w, r, "group_id")
		if !ok {
			return
		}
		if err := requirePLGroupOwner(r, w, database, groupID, userID); err != nil {
			return
		}
		if err := database.DeletePLGroup(r.Context(), groupID); err != nil {
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

// SetPLGroupHandler assigns (or clears) the group for a production line.
func SetPLGroupHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		plID, ok := parseUUIDParam(w, r, "line_id")
		if !ok {
			return
		}
		ownerID, err := database.ProductionLineOwnerUserID(r.Context(), plID)
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
			GroupID *uuid.UUID `json:"group_id"`
		}
		if !decodeJSON(w, r, &body) {
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
		last, err := database.GetLastPLPosition(r.Context(), *pl.FactoryID, body.GroupID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if err := database.MovePLToGroup(r.Context(), plID, body.GroupID, fracidx.Between(last, "")); err != nil {
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

// requirePLGroupOwner verifies the requesting user owns the given PL group.
func requirePLGroupOwner(r *http.Request, w http.ResponseWriter, database *db.DB, groupID, userID uuid.UUID) error {
	ownerID, err := database.PLGroupOwnerUserID(r.Context(), groupID)
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
