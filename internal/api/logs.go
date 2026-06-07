package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/logging"
)

// LogsHandler handles GET /api/logs?n=200
// Returns recent log entries from the in-memory ring buffer.
func LogsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := 200
		if v := r.URL.Query().Get("n"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				n = parsed
			}
		}
		entries := logging.Global.Recent(n)
		if entries == nil {
			entries = []logging.Entry{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(entries); err != nil {
			GoLog.Warnf("logs handler encode: %v", err)
		}
	}
}

// LogsStreamHandler handles GET /api/logs/stream — SSE live tail.
func LogsStreamHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ch := logging.Global.Subscribe()
		defer logging.Global.Unsubscribe(ch)

		for {
			select {
			case <-r.Context().Done():
				return
			case entry, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(entry)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
