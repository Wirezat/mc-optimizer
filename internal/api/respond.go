package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeJSON encodes v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already sent — only option is to log.
		slog.Error("writeJSON encode", "err", err)
	}
}

// writeError writes a JSON error envelope: {"error": "<message>"}.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// errBadRequest writes 400 with a message.
func errBadRequest(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, msg)
}

// errUnauthorized writes 401.
func errUnauthorized(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, "unauthorized")
}

// errForbidden writes 403.
func errForbidden(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "forbidden")
}

// errNotFound writes 404.
func errNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not found")
}

// errConflict writes 409.
func errConflict(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusConflict, msg)
}

// errInternal logs err and writes 500.
func errInternal(w http.ResponseWriter, err error) {
	slog.Error("internal server error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
