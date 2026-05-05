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
)

func main() {
	if err := run(); err != nil {
		GoLog.Errorf("fatal: %v", err)
		os.Exit(1) // only called after run() returns — defers inside run() have already executed
	}
}

func run() error {
	port := os.Getenv("PORT")
	if port == "" {
		GoLog.Warn("PORT is not set, defaulting to 8080")
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	GoLog.Infof("Database: %s", maskPassword(dbURL))

	// dbURL will be wired into the DB layer later on
	_ = dbURL

	mux := http.NewServeMux()

	// Health check — used by load balancers and monitoring
	mux.HandleFunc("GET /api/health", healthHandler)

	// Static frontend files
	mux.Handle("/", http.FileServer(http.Dir("web/pages")))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	srv := &http.Server{
		Addr:           ":" + port,
		Handler:        mux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MiB — Protect against header abuse
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
		Time:   time.Now().Format(time.RFC3339),
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
