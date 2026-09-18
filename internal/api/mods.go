package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/assets"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/importer"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
)

// ListAllMachinesHandler returns all machine types across all mods, grouped by tier-variant
// relationships (see ListAllMachinesGrouped), each with its icon texture resolved.
func ListAllMachinesHandler(database *db.DB, assetsDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machines, err := database.ListAllMachinesGrouped(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		if machines == nil {
			machines = []*model.MachineType{}
		}
		attachMachineTextures(machines, assetsDir)
		if err := attachMachineCosts(r.Context(), database, machines); err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, machines)
	}
}

// attachMachineCosts populates each machine's base operating cost from the variant cache,
// leaving it nil when no variant has been computed for it — the catalog never presents a
// missing measurement as a zero cost.
func attachMachineCosts(ctx context.Context, database *db.DB, machines []*model.MachineType) error {
	for _, m := range machines {
		costs, err := database.GetAnyBaseVariantCosts(ctx, m.ModID, m.MachineID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				continue
			}
			return err
		}
		m.Costs = costs
	}
	return nil
}

// attachMachineTextures populates TextureURL on each machine and each of its Variants by
// resolving the icon file on disk, leaving it nil when none exists.
func attachMachineTextures(machines []*model.MachineType, assetsDir string) {
	for _, m := range machines {
		if url, ok := assets.ResolveMachineTexture(assetsDir, m.ModID, m.MachineID); ok {
			m.TextureURL = &url
		}
		for i := range m.Variants {
			v := &m.Variants[i]
			if url, ok := assets.ResolveMachineTexture(assetsDir, v.ModID, v.MachineID); ok {
				v.TextureURL = &url
			}
		}
	}
}

// ListModsHandler returns all mods, each with its installed plugin's display name and
// wizard flag.
func ListModsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mods, err := database.ListMods(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		if mods == nil {
			mods = []*model.Mod{}
		}
		if err := attachModPlugins(r, database, mods); err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, mods)
	}
}

// attachModPlugins fills Mod.Plugin from the installed plugins, leaving it nil for every
// mod that ships none.
func attachModPlugins(r *http.Request, database *db.DB, mods []*model.Mod) error {
	installed, err := database.ListModPlugins(r.Context())
	if err != nil {
		return err
	}
	byMod := make(map[string]*model.ModPluginInfo, len(installed))
	for _, p := range installed {
		byMod[p.ModID] = &model.ModPluginInfo{
			DisplayName: p.DisplayName,
			Version:     p.Version,
			HasWizard:   p.HasWizard,
		}
	}
	for _, m := range mods {
		m.Plugin = byMod[m.ModID]
	}
	return nil
}

// UpdateModHandler updates all editable fields of a mod.
func UpdateModHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		var body struct {
			Name         *string `json:"name"`
			Description  *string `json:"description"`
			Author       *string `json:"author"`
			License      *string `json:"license"`
			URLSource    *string `json:"url_source"`
			URLModrinth  *string `json:"url_modrinth"`
			URLWiki      *string `json:"url_wiki"`
			URLIssues    *string `json:"url_issues"`
			URLDiscord   *string `json:"url_discord"`
			ModrinthSlug *string `json:"modrinth_slug"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Name != nil {
			*body.Name = strings.TrimSpace(*body.Name)
			if *body.Name == "" {
				errBadRequest(w, "name must not be empty")
				return
			}
		}
		u := model.ModUpdate{
			Name:        body.Name,
			Description: body.Description, Author: body.Author, License: body.License,
			URLSource: body.URLSource, URLModrinth: body.URLModrinth, URLWiki: body.URLWiki,
			URLIssues: body.URLIssues, URLDiscord: body.URLDiscord, ModrinthSlug: body.ModrinthSlug,
		}
		if err := database.UpdateModFull(r.Context(), modID, u); err != nil {
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

// ModrinthPreviewHandler fetches Modrinth metadata for a mod without saving it. Accepts
// optional ?slug= query param to override the lookup slug.
func ModrinthPreviewHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		slugOverride := strings.TrimSpace(r.URL.Query().Get("slug"))

		// Get the mod's display name so lookupProject can search by it.
		mods, err := database.ListMods(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		var displayName string
		for _, m := range mods {
			if m.ModID == modID {
				displayName = m.Name
				break
			}
		}

		meta, err := importer.FetchModrinthMetadata(modID, displayName, slugOverride)
		if err != nil {
			errInternal(w, err)
			return
		}
		if meta == nil {
			errNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, meta)
	}
}

// DeleteModHandler deletes a mod.
func DeleteModHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		if err := database.DeleteMod(r.Context(), modID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			var inUse *db.ErrModInUse
			if errors.As(err, &inUse) {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":    "MOD_IN_USE",
					"message":  "mod is still in use",
					"blockers": inUse.Blockers,
				})
				return
			}
			errInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ListMachinesHandler returns all machine types for a mod.
func ListMachinesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		machines, err := database.ListMachinesByMod(r.Context(), modID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		if machines == nil {
			machines = []*model.MachineType{}
		}
		writeJSON(w, http.StatusOK, machines)
	}
}

// ListModItemsHandler returns all items for a mod.
func ListModItemsHandler(database *db.DB, assetsDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		items, err := database.ListItemsByMod(r.Context(), modID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		if items == nil {
			items = []*model.Item{}
		}
		attachItemTextures(items, assetsDir)
		writeJSON(w, http.StatusOK, items)
	}
}

// ListModFluidsHandler returns all fluids for a mod.
func ListModFluidsHandler(database *db.DB, assetsDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		fluids, err := database.ListFluidsByMod(r.Context(), modID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if fluids == nil {
			fluids = []*model.Fluid{}
		}
		attachFluidTextures(fluids, assetsDir)
		writeJSON(w, http.StatusOK, fluids)
	}
}

// attachItemTextures populates TextureURL on each item by resolving its texture file on
// disk, leaving it nil when no file exists.
func attachItemTextures(items []*model.Item, assetsDir string) {
	for _, it := range items {
		if url, ok := assets.ResolveItemTexture(assetsDir, it.ModID, it.ItemID); ok {
			it.TextureURL = &url
			it.Animation = resolveAnimation(assetsDir, url)
		}
	}
}

// attachFluidTextures populates TextureURL on each fluid by resolving its texture file on
// disk, leaving it nil when no file exists.
func attachFluidTextures(fluids []*model.Fluid, assetsDir string) {
	for _, fl := range fluids {
		if url, ok := assets.ResolveFluidTexture(assetsDir, fl.ModID, fl.FluidID); ok {
			fl.TextureURL = &url
			fl.Animation = resolveAnimation(assetsDir, url)
		}
	}
}

// resolveAnimation describes how to play a texture that turns out to be a sprite sheet, and
// returns nil for an ordinary one so the field stays out of the JSON.
func resolveAnimation(assetsDir, url string) *model.TextureAnimation {
	sheet, ok := assets.ResolveAnimation(assetsDir, url)
	if !ok {
		return nil
	}
	return &model.TextureAnimation{
		Cells:    sheet.Cells,
		Frames:   sheet.Play,
		FrameMS:  sheet.FrameMS,
		PingPong: sheet.PingPong,
	}
}

// splitCatalogRef splits a "mod_id:id" query param into its parts. Returns ("", "") if ref
// is empty or malformed (no colon), which callers treat as "no filter".
func splitCatalogRef(ref string) (modID, id string) {
	modID, id, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if !ok {
		return "", ""
	}
	return modID, id
}

// ListModRecipesHandler returns all recipes for a mod.
func ListModRecipesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		recipes, err := database.ListRecipesByMod(r.Context(), modID, machineID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		if recipes == nil {
			recipes = []*model.Recipe{}
		}
		writeJSON(w, http.StatusOK, recipes)
	}
}

// optionalSaveID parses an optional ?save_id= query param.
func optionalSaveID(r *http.Request) *uuid.UUID {
	raw := r.URL.Query().Get("save_id")
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &id
}

// SearchItemsHandler returns items matching an optional query string across all mods.
func SearchItemsHandler(database *db.DB, assetsDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if producedByMod := r.URL.Query().Get("producedByMod"); producedByMod != "" {
			items, err := database.ListItemsProducedBy(r.Context(), producedByMod, "")
			if err != nil {
				errInternal(w, err)
				return
			}
			if items == nil {
				items = []*model.Item{}
			}
			attachItemTextures(items, assetsDir)
			writeJSON(w, http.StatusOK, items)
			return
		}
		if producedByMachine := r.URL.Query().Get("producedByMachine"); producedByMachine != "" {
			machineMod, machineID := splitCatalogRef(producedByMachine)
			if machineMod == "" || machineID == "" {
				errBadRequest(w, "producedByMachine must be a \"mod_id:machine_id\" pair")
				return
			}
			items, err := database.ListItemsProducedBy(r.Context(), machineMod, machineID)
			if err != nil {
				errInternal(w, err)
				return
			}
			if items == nil {
				items = []*model.Item{}
			}
			attachItemTextures(items, assetsDir)
			writeJSON(w, http.StatusOK, items)
			return
		}
		if r.URL.Query().Get("all") == "true" {
			items, err := database.ListAllItems(r.Context(), optionalSaveID(r))
			if err != nil {
				errInternal(w, err)
				return
			}
			if items == nil {
				items = []*model.Item{}
			}
			attachItemTextures(items, assetsDir)
			writeJSON(w, http.StatusOK, items)
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 {
			offset = 0
		}
		items, err := database.SearchItems(r.Context(), q, offset)
		if err != nil {
			errInternal(w, err)
			return
		}
		if items == nil {
			items = []*model.Item{}
		}
		attachItemTextures(items, assetsDir)
		writeJSON(w, http.StatusOK, items)
	}
}

// SearchFluidsHandler returns fluids matching an optional query string across all mods.
func SearchFluidsHandler(database *db.DB, assetsDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if producedByMod := r.URL.Query().Get("producedByMod"); producedByMod != "" {
			fluids, err := database.ListFluidsProducedBy(r.Context(), producedByMod, "")
			if err != nil {
				errInternal(w, err)
				return
			}
			if fluids == nil {
				fluids = []*model.Fluid{}
			}
			attachFluidTextures(fluids, assetsDir)
			writeJSON(w, http.StatusOK, fluids)
			return
		}
		if producedByMachine := r.URL.Query().Get("producedByMachine"); producedByMachine != "" {
			machineMod, machineID := splitCatalogRef(producedByMachine)
			if machineMod == "" || machineID == "" {
				errBadRequest(w, "producedByMachine must be a \"mod_id:machine_id\" pair")
				return
			}
			fluids, err := database.ListFluidsProducedBy(r.Context(), machineMod, machineID)
			if err != nil {
				errInternal(w, err)
				return
			}
			if fluids == nil {
				fluids = []*model.Fluid{}
			}
			attachFluidTextures(fluids, assetsDir)
			writeJSON(w, http.StatusOK, fluids)
			return
		}
		if r.URL.Query().Get("all") == "true" {
			fluids, err := database.ListAllFluids(r.Context(), optionalSaveID(r))
			if err != nil {
				errInternal(w, err)
				return
			}
			if fluids == nil {
				fluids = []*model.Fluid{}
			}
			attachFluidTextures(fluids, assetsDir)
			writeJSON(w, http.StatusOK, fluids)
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 {
			offset = 0
		}
		fluids, err := database.SearchFluids(r.Context(), q, offset)
		if err != nil {
			errInternal(w, err)
			return
		}
		if fluids == nil {
			fluids = []*model.Fluid{}
		}
		attachFluidTextures(fluids, assetsDir)
		writeJSON(w, http.StatusOK, fluids)
	}
}
