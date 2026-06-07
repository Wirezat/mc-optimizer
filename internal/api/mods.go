package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
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

// UpdateModHandler updates name and/or energy_type of a mod. Admin only (enforced at route level).
func UpdateModHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		if modID == "" {
			errBadRequest(w, "mod_id is required")
			return
		}
		var body struct {
			Name       *string `json:"name"`
			EnergyType *string `json:"energy_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
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
			*body.EnergyType = strings.TrimSpace(*body.EnergyType)
			if *body.EnergyType == "" {
				errBadRequest(w, "energy_type must not be empty")
				return
			}
		}
		err := database.UpdateMod(r.Context(), modID, body.Name, body.EnergyType)
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
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		if body.Name != nil {
			*body.Name = strings.TrimSpace(*body.Name)
			if *body.Name == "" {
				errBadRequest(w, "name must not be empty")
				return
			}
		}
		err := database.UpdateMachineType(r.Context(), modID, machineID, body.Name, body.BaseEUPerTick)
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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
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
func ListModItemsHandler(database *db.DB) http.HandlerFunc {
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
		writeJSON(w, http.StatusOK, items)
	}
}

// ListModRecipesHandler returns all recipes for a mod.
// Optional query param: ?machine_id=<id> to filter by machine.
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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
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
// DELETE /api/recipes/{recipe_id}
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
// GET /api/mods/{mod_id}/machines/{machine_id}/interfaces
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
// POST /api/mods/{mod_id}/machines/{machine_id}/interfaces
func AddMachineInterfaceHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := strings.TrimSpace(r.PathValue("mod_id"))
		machineID := strings.TrimSpace(r.PathValue("machine_id"))
		var body struct {
			BaseModID     string `json:"base_mod_id"`
			BaseMachineID string `json:"base_machine_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
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
// DELETE /api/mods/{mod_id}/machines/{machine_id}/interfaces/{base_mod_id}/{base_machine_id}
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

// SearchItemsHandler returns items matching an optional query string across all mods.
// Optional query param: ?q=<search term>
func SearchItemsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		items, err := database.SearchItems(r.Context(), q)
		if err != nil {
			errInternal(w, err)
			return
		}
		if items == nil {
			items = []*model.Item{}
		}
		writeJSON(w, http.StatusOK, items)
	}
}

// SearchFluidsHandler returns fluids matching an optional query string across all mods.
// Optional query param: ?q=<search term>
func SearchFluidsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		fluids, err := database.SearchFluids(r.Context(), q)
		if err != nil {
			errInternal(w, err)
			return
		}
		if fluids == nil {
			fluids = []*model.Fluid{}
		}
		writeJSON(w, http.StatusOK, fluids)
	}
}
