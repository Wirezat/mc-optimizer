package api

import (
	"encoding/json"
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/google/uuid"
)

// decodeJSON decodes JSON from r.Body into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		errBadRequest(w, "invalid JSON")
		return false
	}
	return true
}

// Error codes — machine-readable keys sent in every error response.
const (
	CodeBadRequest   = "BAD_REQUEST"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeNotFound     = "NOT_FOUND"
	CodeConflict     = "CONFLICT"
	CodeInternal     = "INTERNAL_SERVER_ERROR"
)

// apiError is the standard error envelope for all API responses.
type apiError struct {
	Code    string `json:"error"`   // machine-readable constant
	Message string `json:"message"` // human-readable text
}

// writeJSON encodes v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		GoLog.Warnf("writeJSON encode: %v", err)
	}
}

// writeAPIError writes a structured JSON error response.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Code: code, Message: message})
}

func errBadRequest(w http.ResponseWriter, msg string) {
	writeAPIError(w, http.StatusBadRequest, CodeBadRequest, msg)
}

func errUnauthorized(w http.ResponseWriter) {
	writeAPIError(w, http.StatusUnauthorized, CodeUnauthorized, "unauthorized")
}

func errForbidden(w http.ResponseWriter) {
	writeAPIError(w, http.StatusForbidden, CodeForbidden, "forbidden")
}

func errNotFound(w http.ResponseWriter) {
	writeAPIError(w, http.StatusNotFound, CodeNotFound, "not found")
}

func errConflict(w http.ResponseWriter, msg string) {
	writeAPIError(w, http.StatusConflict, CodeConflict, msg)
}

func errInternal(w http.ResponseWriter, err error) {
	GoLog.Errorf("internal server error: %v", err)
	writeAPIError(w, http.StatusInternalServerError, CodeInternal, "internal server error")
}

// parseUUIDParam parses a named path parameter as a UUID.
func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		errBadRequest(w, name+" must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}
