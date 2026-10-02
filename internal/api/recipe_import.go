package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/importer"
	"github.com/Wirezat/production-optimizer/internal/render"
)

// ImportModFileHandler accepts one or more modfile ZIP uploads and imports them.
func ImportModFileHandler(database *db.DB, assetsDir string, renderCache *render.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(10 * time.Minute))
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Minute))

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
			if errors.Is(err, importer.ErrInvalidModFile) {
				GoLog.Errorf("modfile import %s: %v", h.Filename, err)
				writeAPIError(w, http.StatusInternalServerError, CodeModFileInvalid, fmt.Sprintf("%s: %v", h.Filename, err))
				return
			}
			if err != nil {
				errInternal(w, fmt.Errorf("modfile import %s: %w", h.Filename, err))
				return
			}
			results = append(results, result)
		}
		writeJSON(w, http.StatusOK, results)
	}
}
