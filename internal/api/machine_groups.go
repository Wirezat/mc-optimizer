package api

import (
	"errors"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/service"
	"github.com/google/uuid"
)

// UpdateMachineGroupStatusHandler sets the status of a single machine group.
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
			Status              string `json:"status"`
			BuiltCount          *int   `json:"built_count"`
			CurrentUpgradeCount *int   `json:"current_upgrade_count"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}

		if body.Status != "" {
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
		}

		if body.BuiltCount != nil || body.CurrentUpgradeCount != nil {
			mg, err := database.GetMachineGroup(r.Context(), groupID)
			if err != nil {
				if errors.Is(err, db.ErrNotFound) {
					errNotFound(w)
				} else {
					errInternal(w, err)
				}
				return
			}

			builtCount := mg.BuiltCount
			if body.BuiltCount != nil {
				builtCount = *body.BuiltCount
			}
			if builtCount < 0 || builtCount > mg.Count {
				errBadRequest(w, "built_count must be between 0 and count")
				return
			}

			currentUpgradeCount := mg.CurrentUpgradeCount
			if body.CurrentUpgradeCount != nil {
				currentUpgradeCount = *body.CurrentUpgradeCount
			}
			if currentUpgradeCount < 0 {
				errBadRequest(w, "current_upgrade_count must be >= 0")
				return
			}

			if err := database.UpdateMachineGroupBuildState(r.Context(), groupID, builtCount, currentUpgradeCount); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					errNotFound(w)
				} else {
					errInternal(w, err)
				}
				return
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// UpdateMachineGroupUpgradesHandler sets the upgrade tier+count of a single machine group
// and recomputes its machine count, preserving the group's output rate (variant A).
func UpdateMachineGroupUpgradesHandler(database *db.DB, svc *service.PLService) http.HandlerFunc {
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
			UpgradeTierID *string `json:"upgrade_tier_id"`
			UpgradeCount  int     `json:"upgrade_count"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		var tierID *uuid.UUID
		if body.UpgradeTierID != nil && *body.UpgradeTierID != "" {
			id, err := uuid.Parse(*body.UpgradeTierID)
			if err != nil {
				errBadRequest(w, "upgrade_tier_id must be a UUID")
				return
			}
			tierID = &id
		}
		if body.UpgradeCount < 0 {
			errBadRequest(w, "upgrade_count must be >= 0")
			return
		}
		newCount, err := svc.SetGroupUpgrade(r.Context(), groupID, tierID, body.UpgradeCount)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": newCount})
	}
}
