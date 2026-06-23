package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

func ListFactorySourceInputsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		inputs, err := database.ListFactorySourceInputs(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if inputs == nil {
			inputs = []*model.FactorySource{}
		}
		writeJSON(w, http.StatusOK, inputs)
	}
}

func UpsertFactorySourceInputHandler(database *db.DB) http.HandlerFunc {
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
		o, err := database.UpsertFactorySourceInput(r.Context(), factoryID, body.ModID, body.ItemID, body.RateNum, body.RateDen, body.TimeUnit)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, o)
	}
}

func DeleteFactorySourceInputHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		inputID, ok := parseUUIDParam(w, r, "input_id")
		if !ok {
			return
		}
		ownerID, err := database.FactorySourceInputOwnerUserID(r.Context(), inputID)
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
		if err := database.DeleteFactorySourceInput(r.Context(), inputID); err != nil {
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
