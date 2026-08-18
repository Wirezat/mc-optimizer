package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/render"
)

// Bounds on the size a render may be asked for, so a request cannot make the
// server rasterise something absurd or blow up the per-model render cache.
const (
	minRenderSize = 8
	maxRenderSize = 256
)

const defaultRenderSize = 32

// etagMatches reports whether an If-None-Match header lists this ETag.
//
// Compares entries rather than substrings: a substring test would treat any
// header merely containing the tag as a match, and "*" means "any current
// representation", which for a resource that exists is a match.
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

// RenderModelHandler serves an inventory-style icon for a block model, rendered
// on demand.
//
// Path: /assets/render/{mod}/{model...}.png?size=32
// e.g. /assets/render/modern_industrialization/block/pipes/copper_cable.png
//
// Public like the rest of the asset tree: these are game assets, and the pages
// that show them are reachable before login.
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

		// The bytes only change when an import replaces the model or its
		// textures, and the ETag follows the bytes — so a client can hold on to
		// an icon indefinitely and still pick up a re-import.
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
