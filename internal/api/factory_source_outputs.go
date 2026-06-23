package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListFactorySourceOutputsHandler returns all manual outputs for a source factory.
func ListFactorySourceOutputsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		outputs, err := database.ListFactorySourceOutputs(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if outputs == nil {
			outputs = []*model.FactorySource{}
		}
		writeJSON(w, http.StatusOK, outputs)
	}
}

// UpsertFactorySourceOutputHandler creates or updates a source factory output entry.
func UpsertFactorySourceOutputHandler(database *db.DB) http.HandlerFunc {
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
			ModID    string `json:"mod_id"`
			ItemID   string `json:"item_id"`
			RateNum  int    `json:"rate_num"`
			RateDen  int    `json:"rate_den"`
			TimeUnit string `json:"time_unit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if body.ModID == "" || body.ItemID == "" {
			errBadRequest(w, "mod_id and item_id are required")
			return
		}
		if body.RateDen == 0 {
			body.RateDen = 1
		}
		if body.TimeUnit == "" {
			body.TimeUnit = "min"
		}
		o, err := database.UpsertFactorySourceOutput(r.Context(), factoryID, body.ModID, body.ItemID, body.RateNum, body.RateDen, body.TimeUnit)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, o)
	}
}

// DeleteFactorySourceOutputHandler removes a single source factory output entry.
func DeleteFactorySourceOutputHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		outputID, ok := parseUUIDParam(w, r, "output_id")
		if !ok {
			return
		}
		ownerID, err := database.FactorySourceOutputOwnerUserID(r.Context(), outputID)
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
		if err := database.DeleteFactorySourceOutput(r.Context(), outputID); err != nil {
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

