package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	auth "github.com/Wirezat/production-optimizer/internal/crypto"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/google/uuid"
)

type contextKey int

const (
	contextKeyUserID  contextKey = iota
	contextKeyIsAdmin contextKey = iota
	contextKeyIsOwner contextKey = iota
)

// RequireAuth validates Bearer token, injects userID and isAdmin into context, else 401.
func RequireAuth(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				GoLog.Warnf("auth: no bearer token on %s %s", r.Method, r.URL.Path)
				errUnauthorized(w)
				return
			}
			token, err := database.GetTokenByHash(r.Context(), auth.HashToken(raw))
			if err != nil {
				GoLog.Warnf("auth: token not found on %s %s err=%v", r.Method, r.URL.Path, err)
				errUnauthorized(w)
				return
			}
			if token.Type != db.TokenTypeSession {
				GoLog.Warnf("auth: wrong token type %q on %s %s", token.Type, r.Method, r.URL.Path)
				errUnauthorized(w)
				return
			}
			if token.ExpiresAt.Before(time.Now()) {
				GoLog.Warnf("auth: token expired at %s on %s %s", token.ExpiresAt, r.Method, r.URL.Path)
				errUnauthorized(w)
				return
			}
			user, err := database.GetUserByID(r.Context(), token.UserID)
			if err != nil {
				errUnauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), contextKeyUserID, token.UserID)
			ctx = context.WithValue(ctx, contextKeyIsAdmin, user.IsAdmin || user.IsOwner)
			ctx = context.WithValue(ctx, contextKeyIsOwner, user.IsOwner)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin returns 403 if the authenticated user is not an admin.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAdminFromContext(r.Context()) {
			errForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// userIDFromContext retrieves userID from context; panics if RequireAuth missing (programming error).
func userIDFromContext(ctx context.Context) uuid.UUID {
	id, ok := ctx.Value(contextKeyUserID).(uuid.UUID)
	if !ok {
		panic("api: userIDFromContext called without RequireAuth middleware")
	}
	return id
}

// isAdminFromContext returns true if the authenticated user is an admin.
func isAdminFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(contextKeyIsAdmin).(bool)
	return v
}

// RequireOwner returns 403 if the authenticated user is not the owner.
func RequireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isOwnerFromContext(r.Context()) {
			errForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isOwnerFromContext returns true if the authenticated user is the server owner.
func isOwnerFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(contextKeyIsOwner).(bool)
	return v
}

// bearerToken extracts raw token from "Authorization: Bearer <token>", empty if absent/malformed.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
