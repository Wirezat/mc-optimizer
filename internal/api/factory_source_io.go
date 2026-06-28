package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
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
		items, err := database.ListFactorySourceInputs(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if items == nil {
			items = []*model.FactorySource{}
		}
		writeJSON(w, http.StatusOK, items)
	}
}

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
		items, err := database.ListFactorySourceOutputs(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if items == nil {
			items = []*model.FactorySource{}
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func UpsertFactorySourceInputHandler(database *db.DB) http.HandlerFunc {
	return upsertFactorySource(database, database.UpsertFactorySourceInput)
}

func UpsertFactorySourceOutputHandler(database *db.DB) http.HandlerFunc {
	return upsertFactorySource(database, database.UpsertFactorySourceOutput)
}

func DeleteFactorySourceInputHandler(database *db.DB) http.HandlerFunc {
	return deleteFactorySource(database, "input_id", database.FactorySourceInputOwnerUserID, database.DeleteFactorySourceInput)
}

func DeleteFactorySourceOutputHandler(database *db.DB) http.HandlerFunc {
	return deleteFactorySource(database, "output_id", database.FactorySourceOutputOwnerUserID, database.DeleteFactorySourceOutput)
}

type upsertFn func(ctx context.Context, factoryID uuid.UUID, modID, itemID string, rateNum, rateDen int, timeUnit string) (*model.FactorySource, error)

func upsertFactorySource(database *db.DB, fn upsertFn) http.HandlerFunc {
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
		if !decodeJSON(w, r, &body) {
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
		o, err := fn(r.Context(), factoryID, body.ModID, body.ItemID, body.RateNum, body.RateDen, body.TimeUnit)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, o)
	}
}

type ownerFn func(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
type deleteFn func(ctx context.Context, id uuid.UUID) error

func deleteFactorySource(database *db.DB, idParam string, ownerOf ownerFn, del deleteFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		id, ok := parseUUIDParam(w, r, idParam)
		if !ok {
			return
		}
		ownerID, err := ownerOf(r.Context(), id)
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
		if err := del(r.Context(), id); err != nil {
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
