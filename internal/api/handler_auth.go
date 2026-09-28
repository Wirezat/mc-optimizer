package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	auth "github.com/Wirezat/production-optimizer/internal/crypto"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
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

// issueTokensForUser generates, persists, and returns an access+refresh token pair for the
// given user.
func issueTokensForUser(r *http.Request, database *db.DB, u *model.User) (*tokenPair, error) {
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

// AuthConfigHandler handles GET /api/auth/config — public, no auth required.
func AuthConfigHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enabled, err := database.RegistrationEnabled(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		hasOwner, err := database.HasOwner(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"registration_enabled": enabled,
			"has_owner":            hasOwner,
		})
	}
}

// RegisterHandler handles POST /api/auth/register.
func RegisterHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if enabled, err := database.RegistrationEnabled(r.Context()); err != nil {
			errInternal(w, err)
			return
		} else if !enabled {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":   "REGISTRATION_DISABLED",
				"message": "Registration is currently disabled by the administrator.",
			})
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			errBadRequest(w, "username and password are required")
			return
		}
		if len(req.Password) < 8 {
			errBadRequest(w, "password must be at least 8 characters")
			return
		}
		ip := clientIP(r)
		if !allowAll(w, r, "register", limitCheck{registerIPLimiter, ip}) {
			return
		}
		registerIPLimiter.record(ip)
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			errInternal(w, err)
			return
		}
		u, err := database.CreateUser(r.Context(), req.Username, hash)
		if err != nil {
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "username or email already taken")
				return
			}
			errInternal(w, err)
			return
		}
		promoted, err := database.PromoteToOwnerIfFirst(r.Context(), u.ID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if promoted {
			u.IsOwner = true
			u.IsAdmin = true
		}
		pair, err := issueTokensForUser(r, database, u)
		if err != nil {
			errInternal(w, err)
			return
		}
		type registerResponse struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			IsFirstUser  bool   `json:"is_first_user,omitempty"`
		}
		writeJSON(w, http.StatusCreated, registerResponse{
			AccessToken:  pair.AccessToken,
			RefreshToken: pair.RefreshToken,
			IsFirstUser:  promoted,
		})
	}
}

// LoginHandler handles POST /api/auth/login.
func LoginHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		ip := clientIP(r)
		if !allowAll(w, r, "login",
			limitCheck{loginIPLimiter, ip},
			limitCheck{loginAccountLimiter, req.Username}) {
			return
		}
		fail := func() {
			loginIPLimiter.record(ip)
			loginAccountLimiter.record(req.Username)
			errUnauthorized(w)
		}
		u, err := database.GetUserByUsername(r.Context(), req.Username)
		if err != nil {
			GoLog.Warnf("login: user not found: %q err=%v", req.Username, err)
			fail()
			return
		}
		if err := auth.VerifyPassword(u.PasswordHash, req.Password); err != nil {
			GoLog.Warnf("login: wrong password for user %q", req.Username)
			fail()
			return
		}
		loginIPLimiter.reset(ip)
		loginAccountLimiter.reset(req.Username)
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
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.RefreshToken == "" {
			errBadRequest(w, "refresh_token is required")
			return
		}
		ip := clientIP(r)
		if !allowAll(w, r, "refresh", limitCheck{refreshIPLimiter, ip}) {
			return
		}
		hashed := auth.HashToken(req.RefreshToken)
		token, err := database.GetTokenByHash(r.Context(), hashed)
		if err != nil || token.Type != db.TokenTypeRefresh {
			refreshIPLimiter.record(ip)
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

// LogoutHandler handles POST /api/auth/logout — invalidates access and optional refresh
// token.
func LogoutHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if raw := bearerToken(r); raw != "" {
			_ = database.DeleteToken(r.Context(), auth.HashToken(raw))
		}
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			GoLog.Warnf("logout: failed to decode body: %v", err)
		} else if body.RefreshToken != "" {
			_ = database.DeleteToken(r.Context(), auth.HashToken(body.RefreshToken))
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
	}
}
