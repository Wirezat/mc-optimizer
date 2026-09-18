package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/render"
)

// Bounds on the size a render may be asked for.
const (
	minRenderSize = 8
	maxRenderSize = 256
)

const defaultRenderSize = 32

// etagMatches reports whether an If-None-Match header lists this ETag.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

// RenderModelHandler serves an inventory-style icon for a block model, rendered on demand.
func RenderModelHandler(cache *render.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/assets/render/")
		rest = strings.TrimSuffix(rest, ".png")
		modID, model, found := strings.Cut(rest, "/")
		if !found || modID == "" || model == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		size := defaultRenderSize
		if raw := r.URL.Query().Get("size"); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < minRenderSize || v > maxRenderSize {
				http.Error(w, "unsupported size", http.StatusBadRequest)
				return
			}
			size = v
		}

		png, etag, err := cache.Get(modID+":"+model, size)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			// No Content-Type on a 304: it carries no body to describe.
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}
}
