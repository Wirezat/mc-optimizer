package api

import (
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

// ListAllMachinesHandler returns all machine types across all mods, grouped
// by tier-variant relationships (see ListAllMachinesGrouped), each with its
// icon texture resolved.
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
		writeJSON(w, http.StatusOK, machines)
	}
}

// attachMachineTextures populates TextureURL on each machine and each of its
// Variants by resolving the icon file on disk, leaving it nil when none exists.
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

// ListUpgradeTiersHandler returns all upgrade tiers for the solve UI's tier picker.
func ListUpgradeTiersHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tiers, err := database.ListUpgradeTiers(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		if tiers == nil {
			tiers = []model.UpgradeTier{}
		}
		writeJSON(w, http.StatusOK, tiers)
	}
}

// ListModsHandler returns all mods.
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
		writeJSON(w, http.StatusOK, mods)
	}
}

// CreateModHandler creates a new mod. Admin only (enforced at route level).
func CreateModHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ModID      string `json:"mod_id"`
			Name       string `json:"name"`
			EnergyType string `json:"energy_type"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.ModID = strings.TrimSpace(body.ModID)
		body.Name = strings.TrimSpace(body.Name)
		body.EnergyType = strings.TrimSpace(body.EnergyType)
		if body.ModID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		if body.Name == "" {
			errBadRequest(w, "name is required")
			return
		}
		if body.EnergyType == "" {
			errBadRequest(w, "energy_type is required")
			return
		}
		m, err := database.CreateMod(r.Context(), body.ModID, body.Name, body.EnergyType)
		if err != nil {
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "mod_id already exists")
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, m)
	}
}

// UpdateModHandler updates all editable fields of a mod. Admin only (enforced at route level).
func UpdateModHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		var body struct {
			Name         *string `json:"name"`
			EnergyType   *string `json:"energy_type"`
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
		if body.EnergyType != nil {
			*body.EnergyType = strings.ToUpper(strings.TrimSpace(*body.EnergyType))
			if *body.EnergyType == "" {
				errBadRequest(w, "energy_type must not be empty")
				return
			}
		}
		u := model.ModUpdate{
			Name: body.Name, EnergyType: body.EnergyType,
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

// ModrinthPreviewHandler fetches Modrinth metadata for a mod without saving it.
// Accepts optional ?slug= query param to override the lookup slug.
// Admin only (enforced at route level).
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

// DeleteModHandler deletes a mod. Admin only (enforced at route level).
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

// UpdateMachineHandler updates the display name and/or base EU/tick of a machine type.
// Admin only (enforced at route level). PATCH /api/mods/{mod_id}/machines/{machine_id}
func UpdateMachineHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		machineID := strings.TrimSpace(r.PathValue("machine_id"))
		if modID == "" || machineID == "" {
			errBadRequest(w, "mod_id and machine_id are required")
			return
		}
		var body struct {
			Name          *string `json:"name"`
			BaseEUPerTick *int64  `json:"base_eu_per_tick"`
			MaxEUPerTick  *int64  `json:"max_eu_per_tick"`
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
		err := database.UpdateMachineType(r.Context(), modID, machineID, body.Name, body.BaseEUPerTick, body.MaxEUPerTick)
		if err != nil {
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

// UpdateItemHandler updates the display name of an item.
// Admin only (enforced at route level). PATCH /api/mods/{mod_id}/items/{item_id}
func UpdateItemHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		itemID := strings.TrimSpace(r.PathValue("item_id"))
		if modID == "" || itemID == "" {
			errBadRequest(w, "mod_id and item_id are required")
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			errBadRequest(w, "name is required")
			return
		}
		err := database.UpdateItem(r.Context(), modID, itemID, body.Name)
		if err != nil {
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

// attachItemTextures populates TextureURL on each item by resolving its
// texture file on disk, leaving it nil when no file exists.
func attachItemTextures(items []*model.Item, assetsDir string) {
	for _, it := range items {
		if url, ok := assets.ResolveItemTexture(assetsDir, it.ModID, it.ItemID); ok {
			it.TextureURL = &url
			it.Animation = resolveAnimation(assetsDir, url)
		}
	}
}

// attachFluidTextures populates TextureURL on each fluid by resolving its
// texture file on disk, leaving it nil when no file exists.
func attachFluidTextures(fluids []*model.Fluid, assetsDir string) {
	for _, fl := range fluids {
		if url, ok := assets.ResolveFluidTexture(assetsDir, fl.ModID, fl.FluidID); ok {
			fl.TextureURL = &url
			fl.Animation = resolveAnimation(assetsDir, url)
		}
	}
}

// resolveAnimation describes how to play a texture that turns out to be a sprite
// sheet, and returns nil for an ordinary one so the field stays out of the JSON.
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

// splitCatalogRef splits a "mod_id:id" query param into its parts. Returns ("", "")
// if ref is empty or malformed (no colon), which callers treat as "no filter".
func splitCatalogRef(ref string) (modID, id string) {
	modID, id, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if !ok {
		return "", ""
	}
	return modID, id
}

// ListRecipesCatalogHandler returns recipes for the catalog page with optional ?mod= and ?machine= filters.
// Returns recipes without IO details (IO is fetched per-recipe on expand).
func ListRecipesCatalogHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.URL.Query().Get("mod"))
		machineID := strings.TrimSpace(r.URL.Query().Get("machine"))
		itemModID, itemID := splitCatalogRef(r.URL.Query().Get("item"))
		fluidModID, fluidID := splitCatalogRef(r.URL.Query().Get("fluid"))
		recipes, err := database.ListRecipesCatalog(r.Context(), modID, machineID, itemModID, itemID, fluidModID, fluidID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if recipes == nil {
			recipes = []*model.Recipe{}
		}
		writeJSON(w, http.StatusOK, recipes)
	}
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

// CreateModRecipeHandler creates a new recipe within a mod.
// Admin only (enforced at route level).
func CreateModRecipeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		var body model.CreateRecipeRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.MachineID) == "" {
			errBadRequest(w, "machine_id is required")
			return
		}
		if body.DurationTicks <= 0 {
			errBadRequest(w, "duration_ticks must be positive")
			return
		}
		if body.EUPerTick < 0 {
			errBadRequest(w, "eu_per_tick must be non-negative")
			return
		}
		recipe, err := database.CreateRecipe(r.Context(), modID, &body)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, recipe)
	}
}

// UpdateRecipeNameHandler sets or clears the display name of a recipe.
// Admin only (enforced at route level). PATCH /api/recipes/{recipe_id}
func UpdateRecipeNameHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID := strings.TrimSpace(r.PathValue("recipe_id"))
		if recipeID == "" {
			errBadRequest(w, "recipe_id is required")
			return
		}
		var body struct {
			Name *string `json:"name"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		// empty string → treat as clear (nil)
		var name *string
		if body.Name != nil {
			trimmed := strings.TrimSpace(*body.Name)
			if trimmed != "" {
				name = &trimmed
			}
		}
		err := database.UpdateRecipeName(r.Context(), recipeID, name)
		if err != nil {
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

// DeleteRecipeHandler removes a recipe. Admin only (enforced at route level).
func DeleteRecipeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID := strings.TrimSpace(r.PathValue("recipe_id"))
		if recipeID == "" {
			errBadRequest(w, "recipe_id is required")
			return
		}
		if err := database.DeleteRecipe(r.Context(), recipeID); err != nil {
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

// ListMachineInterfacesHandler lists all interfaces (base machines) for a machine.
func ListMachineInterfacesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		machineID := strings.TrimSpace(r.PathValue("machine_id"))
		ifaces, err := database.ListMachineInterfaces(r.Context(), modID, machineID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if ifaces == nil {
			ifaces = []db.MachineInterface{}
		}
		writeJSON(w, http.StatusOK, ifaces)
	}
}

// AddMachineInterfaceHandler adds an "implements" relationship.
// Admin only (enforced at route level).
func AddMachineInterfaceHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		machineID := strings.TrimSpace(r.PathValue("machine_id"))
		var body struct {
			BaseModID     string `json:"base_mod_id"`
			BaseMachineID string `json:"base_machine_id"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.BaseModID == "" || body.BaseMachineID == "" {
			errBadRequest(w, "base_mod_id and base_machine_id are required")
			return
		}
		err := database.AddMachineInterface(r.Context(), modID, machineID, body.BaseModID, body.BaseMachineID)
		if err != nil {
			if errors.Is(err, db.ErrConflict) {
				errConflict(w, "interface already exists")
				return
			}
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

// DeleteMachineInterfaceHandler removes an "implements" relationship.
// Admin only (enforced at route level).
func DeleteMachineInterfaceHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		machineID := strings.TrimSpace(r.PathValue("machine_id"))
		baseModID := strings.TrimSpace(r.PathValue("base_mod_id"))
		baseMachineID := strings.TrimSpace(r.PathValue("base_machine_id"))
		err := database.DeleteMachineInterface(r.Context(), modID, machineID, baseModID, baseMachineID)
		if err != nil {
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

// optionalSaveID parses an optional ?save_id= query param. Returns nil (no
// filter) if absent or malformed — this endpoint is also used by /demo/solve,
// which has no save, so a missing/bad save_id must never be a hard error.
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
// ?all=true  → returns all items (catalog use, no limit)
// ?all=true&save_id=<id> → same, restricted to mods active for that save (Solve target picker)
// ?q=&offset → paginated search (autocomplete use, LIMIT 50)
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
// Optional query params: ?q=<search term>&offset=<int>&all=true&save_id=<id>
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
