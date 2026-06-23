package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
)

// ListUsersHandler handles GET /api/admin/users.
func ListUsersHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := database.ListUsers(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		type userView struct {
			ID        string `json:"id"`
			Username  string `json:"username"`
			IsAdmin   bool   `json:"is_admin"`
			IsOwner   bool   `json:"is_owner"`
			CreatedAt string `json:"created_at"`
		}
		out := make([]userView, len(users))
		for i, u := range users {
			out[i] = userView{
				ID:        u.ID.String(),
				Username:  u.Username,
				IsAdmin:   u.IsAdmin || u.IsOwner,
				IsOwner:   u.IsOwner,
				CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// UpdateUserHandler handles PATCH /api/admin/users/{user_id}.
// Allows admins to change username or admin status; Owner account is immutable.
func UpdateUserHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := parseUUIDParam(w, r, "user_id")
		if !ok {
			return
		}
		target, err := database.GetUserByID(r.Context(), userID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		if target.IsOwner {
			errBadRequest(w, "the owner account cannot be modified")
			return
		}

		callerID := userIDFromContext(r.Context())
		if target.ID == callerID {
			errBadRequest(w, "cannot modify your own account")
			return
		}

		var body struct {
			Username *string `json:"username"`
			IsAdmin  *bool   `json:"is_admin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}

		if body.Username != nil {
			name := strings.TrimSpace(*body.Username)
			if name == "" {
				errBadRequest(w, "username cannot be empty")
				return
			}
			if err := database.UpdateUsername(r.Context(), userID, name); err != nil {
				if errors.Is(err, db.ErrConflict) {
					errConflict(w, "username already taken")
				} else {
					errInternal(w, err)
				}
				return
			}
		}
		if body.IsAdmin != nil {
			if err := database.SetUserAdmin(r.Context(), userID, *body.IsAdmin); err != nil {
				errInternal(w, err)
				return
			}
		}

		updated, err := database.GetUserByID(r.Context(), userID)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         updated.ID.String(),
			"username":   updated.Username,
			"is_admin":   updated.IsAdmin || updated.IsOwner,
			"is_owner":   updated.IsOwner,
			"created_at": updated.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
}

// DeleteUserHandler handles DELETE /api/admin/users/{user_id}.
// Owner and the calling user cannot be deleted.
func DeleteUserHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := parseUUIDParam(w, r, "user_id")
		if !ok {
			return
		}
		target, err := database.GetUserByID(r.Context(), userID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		if target.IsOwner {
			errBadRequest(w, "the owner account cannot be deleted")
			return
		}
		callerID := userIDFromContext(r.Context())
		if target.ID == callerID {
			errBadRequest(w, "cannot delete your own account")
			return
		}
		if err := database.DeleteUser(r.Context(), userID); err != nil {
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

// TransferOwnershipHandler handles PUT /api/admin/users/{user_id}/owner.
// Only the current owner may call this. Atomically transfers ownership to the target user.
func TransferOwnershipHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		toID, ok := parseUUIDParam(w, r, "user_id")
		if !ok {
			return
		}
		fromID := userIDFromContext(r.Context())
		if fromID == toID {
			errBadRequest(w, "you are already the owner")
			return
		}
		if err := database.TransferOwnership(r.Context(), fromID, toID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
