package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/api"
	"github.com/Wirezat/production-optimizer/internal/db"
)

func main() {
	if err := run(); err != nil {
		GoLog.Errorf("fatal: %v", err)
		os.Exit(1) // only called after run() returns — defers inside run() have already executed
	}
}

func run() error {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		GoLog.Warn("SERVER_PORT is not set, defaulting to 8080")
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}

	ctx := context.Background()
	database, err := db.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer database.Close()
	GoLog.Infof("Database: %s", maskPassword(dbURL))

	// ── Background cleanup ticker ─────────────────────────────────────────
	// Purges expired tokens (and solver_drafts once that table is in use).
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			n, err := database.DeleteExpiredTokens(context.Background())
			if err != nil {
				GoLog.Errorf("cleanup: delete expired tokens: %v", err)
			} else if n > 0 {
				GoLog.Infof("cleanup: deleted expired tokens: count=%d", n)
			}
		}
	}()

	mux := http.NewServeMux()

	// Health check — used by load balancers and monitoring
	mux.HandleFunc("GET /api/health", healthHandler)

	// Auth (no auth middleware)
	mux.HandleFunc("POST /api/auth/register", api.RegisterHandler(database))
	mux.HandleFunc("POST /api/auth/login", api.LoginHandler(database))
	mux.HandleFunc("POST /api/auth/refresh", api.RefreshHandler(database))
	mux.HandleFunc("POST /api/auth/logout", api.LogoutHandler(database))

	// Protected routes — wrap with RequireAuth middleware
	protected := api.RequireAuth(database)

	mux.Handle("GET /api/saves", protected(api.ListSavesHandler(database)))
	mux.Handle("POST /api/saves", protected(api.CreateSaveHandler(database)))
	mux.Handle("DELETE /api/saves/{id}", protected(api.DeleteSaveHandler(database)))

	mux.Handle("GET /api/saves/{id}/factories", protected(api.ListFactoriesHandler(database)))
	mux.Handle("POST /api/saves/{id}/factories", protected(api.CreateFactoryHandler(database)))
	mux.Handle("GET /api/factories/{id}", protected(api.GetFactoryHandler(database)))
	mux.Handle("DELETE /api/factories/{id}", protected(api.DeleteFactoryHandler(database)))

	// Static frontend files
	mux.Handle("/", http.FileServer(http.Dir("web/pages")))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	srv := &http.Server{
		Addr:           ":" + port,
		Handler:        mux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MiB — protect against header abuse
	}

	// Capture SIGINT / SIGTERM for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		GoLog.Infof("Server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		GoLog.Infof("Received signal %s — shutting down gracefully", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		GoLog.Info("Server stopped cleanly")
	}

	return nil
}

// healthHandler responds with the current server time.
// GET /api/health → 200 {"status":"ok","time":"..."}
func healthHandler(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Status string `json:"status"`
		Time   string `json:"time"`
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response{
		Status: "ok",
		Time:   time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		GoLog.Warnf("health encode error: %v", err)
	}
}

// maskPassword redacts the password in a Postgres DSN for safe logging.
// Falls back to the raw URL if parsing fails.
func maskPassword(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable DSN>"
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	// Show only host:port, not the full DSN
	return net.JoinHostPort(u.Hostname(), u.Port()) + u.Path
}
