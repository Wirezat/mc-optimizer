package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	auth "github.com/Wirezat/production-optimizer/internal/crypto"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/google/uuid"
)

type contextKey int

const contextKeyUserID contextKey = iota

// RequireAuth validates Bearer token, injects userID into context, else 401.
func RequireAuth(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				errUnauthorized(w)
				return
			}
			token, err := database.GetTokenByHash(r.Context(), auth.HashToken(raw))
			if err != nil || token.Type != db.TokenTypeSession || token.ExpiresAt.Before(time.Now()) {
				errUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKeyUserID, token.UserID)))
		})
	}
}

// userIDFromContext retrieves userID from context; panics if RequireAuth missing (programming error).
func userIDFromContext(ctx context.Context) uuid.UUID {
	id, ok := ctx.Value(contextKeyUserID).(uuid.UUID)
	if !ok {
		panic("api: userIDFromContext called without RequireAuth middleware")
	}
	return id
}

// bearerToken extracts raw token from "Authorization: Bearer <token>", empty if absent/malformed.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
