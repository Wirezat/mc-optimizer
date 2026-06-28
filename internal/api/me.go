package api

import (
	"errors"
	"net/http"

	auth "github.com/Wirezat/production-optimizer/internal/crypto"
	"github.com/Wirezat/production-optimizer/internal/db"
)

// MeHandler returns the authenticated user's profile (no password hash).
func MeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromContext(r.Context())
		user, err := database.GetUserByID(r.Context(), uid)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
			} else {
				errInternal(w, err)
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         user.ID,
			"username":   user.Username,
			"is_admin":   user.IsAdmin || user.IsOwner,
			"is_owner":   user.IsOwner,
			"created_at": user.CreatedAt,
		})
	}
}

// ChangeUsernameHandler sets a new username for the authenticated user.
func ChangeUsernameHandler(database *db.DB) http.HandlerFunc {
	type req struct {
		NewUsername string `json:"new_username"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromContext(r.Context())

		var body req
		if !decodeJSON(w, r, &body) {
			return
		}
		if len(body.NewUsername) < 3 {
			errBadRequest(w, "username must be at least 3 characters")
			return
		}

		if err := database.UpdateUsername(r.Context(), uid, body.NewUsername); err != nil {
			switch {
			case errors.Is(err, db.ErrConflict):
				errConflict(w, "username already taken")
			case errors.Is(err, db.ErrNotFound):
				errNotFound(w)
			default:
				errInternal(w, err)
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// ChangePasswordHandler validates current password and sets a new one.
func ChangePasswordHandler(database *db.DB) http.HandlerFunc {
	type req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromContext(r.Context())

		var body req
		if !decodeJSON(w, r, &body) {
			return
		}
		if len(body.NewPassword) < 8 {
			errBadRequest(w, "new password must be at least 8 characters")
			return
		}

		user, err := database.GetUserByID(r.Context(), uid)
		if err != nil {
			errInternal(w, err)
			return
		}

		if err := auth.VerifyPassword(user.PasswordHash, body.CurrentPassword); err != nil {
			errBadRequest(w, "current password is incorrect")
			return
		}

		newHash, err := auth.HashPassword(body.NewPassword)
		if err != nil {
			errInternal(w, err)
			return
		}

		if err := database.UpdatePassword(r.Context(), uid, newHash); err != nil {
			errInternal(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
