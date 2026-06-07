package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
)

// UpdateMachineGroupStatusHandler sets the status of a single machine group.
// PATCH /api/machine-groups/{group_id}/status
// Body: {"status": "planned"|"built"|"archived"}
func UpdateMachineGroupStatusHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		groupID, ok := parseUUIDParam(w, r, "group_id")
		if !ok {
			return
		}
		ownerID, err := database.MachineGroupOwnerUserID(r.Context(), groupID)
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
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		switch body.Status {
		case "planned", "built", "archived":
		default:
			errBadRequest(w, `status must be one of: planned, built, archived`)
			return
		}
		if err := database.UpdateMachineGroupStatus(r.Context(), groupID, body.Status); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
