package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/importer"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListValidRecipeTypesHandler returns all registered recipe type patterns.
func ListValidRecipeTypesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vrts, err := database.ListValidRecipeTypes(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		if vrts == nil {
			vrts = []*model.ValidRecipeType{}
		}
		writeJSON(w, http.StatusOK, vrts)
	}
}

// CreateValidRecipeTypeHandler registers a new valid recipe type pattern.
func CreateValidRecipeTypeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Pattern         string  `json:"pattern"`
			IsRegex         bool    `json:"is_regex"`
			TargetModID     *string `json:"target_mod_id"`
			TargetMachineID *string `json:"target_machine_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if body.Pattern == "" {
			errBadRequest(w, "pattern is required")
			return
		}
		if (body.TargetModID == nil) != (body.TargetMachineID == nil) {
			errBadRequest(w, "target_mod_id and target_machine_id must be set together")
			return
		}

		vrt, err := database.CreateValidRecipeType(r.Context(), body.Pattern, body.IsRegex, body.TargetModID, body.TargetMachineID)
		if err != nil {
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "pattern already registered")
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, vrt)
	}
}

// DeleteValidRecipeTypeHandler removes a valid recipe type by ID.
func DeleteValidRecipeTypeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseUUIDParam(w, r, "vrt_id")
		if !ok {
			return
		}
		if err := database.DeleteValidRecipeType(r.Context(), id); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ImportJARHandler accepts one or more JAR file uploads and imports all recipes,
// translations, tags, textures, block loot tables, and villager trades from them.
// Content-Type: multipart/form-data; field name "jar" (repeatable)
func ImportJARHandler(database *db.DB, assetsDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// JAR imports can be large and slow — extend per-request deadlines.
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(30 * time.Minute))
		_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Minute))

		// Allow up to 512 MiB total for multi-JAR uploads.
		if err := r.ParseMultipartForm(512 << 20); err != nil {
			errBadRequest(w, "invalid multipart form (max 512 MiB)")
			return
		}

		headers := r.MultipartForm.File["jar"]
		if len(headers) == 0 {
			errBadRequest(w, "missing 'jar' file field")
			return
		}

		var tmpPaths []string
		var origNames []string
		defer func() {
			for _, p := range tmpPaths {
				os.Remove(p)
			}
		}()

		for _, h := range headers {
			f, err := h.Open()
			if err != nil {
				errInternal(w, fmt.Errorf("open upload %s: %w", h.Filename, err))
				return
			}
			tmp, err := os.CreateTemp("", "mc-jar-*.jar")
			if err != nil {
				f.Close()
				errInternal(w, fmt.Errorf("create temp file: %w", err))
				return
			}
			_, copyErr := tmp.ReadFrom(f)
			f.Close()
			tmp.Close()
			if copyErr != nil {
				errInternal(w, fmt.Errorf("write temp file for %s: %w", h.Filename, copyErr))
				return
			}
			tmpPaths = append(tmpPaths, tmp.Name())
			origNames = append(origNames, h.Filename)
		}

		imp := importer.New(database, assetsDir)
		result, err := imp.Run(r.Context(), tmpPaths)
		if err != nil {
			errInternal(w, fmt.Errorf("import: %w", err))
			return
		}

		result.JARs = origNames
		writeJSON(w, http.StatusOK, result)
	}
}

// ImportStatusHandler returns which root mod namespaces are present in the catalog.
func ImportStatusHandler(database *db.DB) http.HandlerFunc {
	type status struct {
		Minecraft bool `json:"minecraft"`
		Forge     bool `json:"forge"`
		NeoForge  bool `json:"neoforge"`
		Fabric    bool `json:"fabric"` // "c" namespace
		TotalMods int  `json:"total_mods"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.Pool.Query(r.Context(), `SELECT mod_id FROM mods`)
		if err != nil {
			errInternal(w, err)
			return
		}
		defer rows.Close()

		s := status{}
		for rows.Next() {
			var modID string
			if err := rows.Scan(&modID); err != nil {
				errInternal(w, err)
				return
			}
			s.TotalMods++
			switch modID {
			case "minecraft":
				s.Minecraft = true
			case "forge":
				s.Forge = true
			case "neoforge":
				s.NeoForge = true
			case "c":
				s.Fabric = true
			}
		}
		if err := rows.Err(); err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s)
	}
}

