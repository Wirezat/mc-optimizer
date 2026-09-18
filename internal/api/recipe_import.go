package api

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/importer"
	"github.com/Wirezat/production-optimizer/internal/render"
)

// ImportModFileHandler accepts one or more modfile ZIP uploads and imports them.
// Content-Type: multipart/form-data; field name "modfile" (repeatable)
//
// renderCache is dropped once the import finishes: an import overwrites models
// and textures at paths the cache has already rendered from, and it holds those
// renders for the process's lifetime. Without this an icon would keep showing
// the pre-import geometry until a restart.
func ImportModFileHandler(database *db.DB, assetsDir string, renderCache *render.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(10 * time.Minute))
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Minute))

		// Deferred, not called after the loop: a later file can fail and return
		// early once an earlier one already overwrote rendered assets.
		if renderCache != nil {
			defer renderCache.Invalidate()
		}

		if err := r.ParseMultipartForm(64 << 20); err != nil {
			errBadRequest(w, "invalid multipart form (max 64 MiB)")
			return
		}
		headers := r.MultipartForm.File["modfile"]
		if len(headers) == 0 {
			errBadRequest(w, "missing 'modfile' file field")
			return
		}

		// Resolved once per request and attributed to any plugin a ZIP in
		// this batch bundles; empty if the acting user cannot be resolved.
		var uploadedBy string
		if u, err := database.GetUserByID(r.Context(), userIDFromContext(r.Context())); err == nil {
			uploadedBy = u.Username
		}

		var results []importer.ModFileResult
		for _, h := range headers {
			f, err := h.Open()
			if err != nil {
				errInternal(w, fmt.Errorf("open upload %s: %w", h.Filename, err))
				return
			}
			tmp, err := os.CreateTemp("", "mc-modfile-*.zip")
			if err != nil {
				f.Close()
				errInternal(w, fmt.Errorf("create temp file: %w", err))
				return
			}
			tmpName := tmp.Name()
			_, copyErr := tmp.ReadFrom(f)
			f.Close()
			tmp.Close()
			defer os.Remove(tmpName)

			if copyErr != nil {
				errInternal(w, fmt.Errorf("write temp file %s: %w", h.Filename, copyErr))
				return
			}

			imp := importer.New(database, assetsDir)
			imp.UploadedBy = uploadedBy
			result, err := imp.RunModFile(r.Context(), tmpName)
			if err != nil {
				errInternal(w, fmt.Errorf("modfile import %s: %w", h.Filename, err))
				return
			}
			results = append(results, result)
		}
		writeJSON(w, http.StatusOK, results)
	}
}
