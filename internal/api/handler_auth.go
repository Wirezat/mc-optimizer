package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	auth "github.com/Wirezat/production-optimizer/internal/crypto"
	"github.com/Wirezat/production-optimizer/internal/db"
)

func ttl(env string, def time.Duration) time.Duration {
	if v := os.Getenv(env); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func accessTTL() time.Duration  { return ttl("ACCESS_TOKEN_TTL", 15*time.Minute) }
func refreshTTL() time.Duration { return ttl("REFRESH_TOKEN_TTL", 7*24*time.Hour) }

// tokenPair is the JSON response on successful auth.
type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// issueTokensForUser generates, persists, and returns an access+refresh token pair for the given user.
func issueTokensForUser(r *http.Request, database *db.DB, u *db.User) (*tokenPair, error) {
	now := time.Now().UTC()
	rawAccess, hashedAccess, err := auth.GenerateToken()
	if err != nil {
		return nil, err
	}
	rawRefresh, hashedRefresh, err := auth.GenerateToken()
	if err != nil {
		return nil, err
	}
	if _, err = database.CreateToken(r.Context(), u.ID, hashedAccess, db.TokenTypeSession, now.Add(accessTTL())); err != nil {
		return nil, err
	}
	if _, err = database.CreateToken(r.Context(), u.ID, hashedRefresh, db.TokenTypeRefresh, now.Add(refreshTTL())); err != nil {
		return nil, err
	}
	return &tokenPair{rawAccess, rawRefresh}, nil
}

// RegisterHandler handles POST /api/auth/register.
func RegisterHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Username == "" || req.Email == "" || req.Password == "" {
			errBadRequest(w, "username, email and password are required")
			return
		}
		if len(req.Password) < 8 {
			errBadRequest(w, "password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			errInternal(w, err)
			return
		}
		u, err := database.CreateUser(r.Context(), req.Username, req.Email, hash)
		if err != nil {
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "username or email already taken")
				return
			}
			errInternal(w, err)
			return
		}
		pair, err := issueTokensForUser(r, database, u)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, pair)
	}
}

// LoginHandler handles POST /api/auth/login.
func LoginHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		u, err := database.GetUserByEmail(r.Context(), req.Email)
		if err != nil {
			errUnauthorized(w)
			return
		}
		if err := auth.VerifyPassword(u.PasswordHash, req.Password); err != nil {
			errUnauthorized(w)
			return
		}
		pair, err := issueTokensForUser(r, database, u)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pair)
	}
}

// RefreshHandler handles POST /api/auth/refresh — rotates refresh token, issues new pair.
func RefreshHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if req.RefreshToken == "" {
			errBadRequest(w, "refresh_token is required")
			return
		}
		hashed := auth.HashToken(req.RefreshToken)
		token, err := database.GetTokenByHash(r.Context(), hashed)
		if err != nil || token.Type != db.TokenTypeRefresh {
			errUnauthorized(w)
			return
		}
		if err := database.DeleteToken(r.Context(), hashed); err != nil {
			errInternal(w, err)
			return
		}
		u, err := database.GetUserByID(r.Context(), token.UserID)
		if err != nil {
			errInternal(w, err)
			return
		}
		pair, err := issueTokensForUser(r, database, u)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, pair)
	}
}

// LogoutHandler handles POST /api/auth/logout — invalidates access and optional refresh token.
func LogoutHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if raw := bearerToken(r); raw != "" {
			_ = database.DeleteToken(r.Context(), auth.HashToken(raw))
		}
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.RefreshToken != "" {
			_ = database.DeleteToken(r.Context(), auth.HashToken(body.RefreshToken))
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
	}
}
