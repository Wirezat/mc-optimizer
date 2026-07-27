package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

		// Deferred, not called after the loop: a later file in a multi-file
		// upload can fail and return early, after an earlier one already
		// overwrote assets the cache rendered from. Invalidating on an upload
		// that imported nothing is harmless — the next request just pays for
		// a fresh render instead of a cache hit.
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

			// Archive the uploaded ZIP in assetsDir/uploads/ for operator reference.
			base := strings.TrimSuffix(filepath.Base(h.Filename), ".zip")
			archiveName := fmt.Sprintf("%s_%d.zip", base, time.Now().Unix())
			archiveDir := filepath.Join(assetsDir, "uploads")
			_ = os.MkdirAll(archiveDir, 0o755)
			if src, err2 := os.Open(tmpName); err2 == nil {
				if dst, err3 := os.Create(filepath.Join(archiveDir, archiveName)); err3 == nil {
					_, _ = io.Copy(dst, src)
					dst.Close()
				}
				src.Close()
			}
			if copyErr != nil {
				errInternal(w, fmt.Errorf("write temp file %s: %w", h.Filename, copyErr))
				return
			}

			imp := importer.New(database, assetsDir)
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

// ImportStatusHandler returns total mod count and presence of core namespaces.
func ImportStatusHandler(database *db.DB) http.HandlerFunc {
	type status struct {
		TotalMods int `json:"total_mods"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var n int
		err := database.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM mods`).Scan(&n)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status{TotalMods: n})
	}
}
