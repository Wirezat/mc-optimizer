package api

import (
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
)

// GetFluidRecipesHandler handles GET /api/fluids/{mod_id}/{fluid_id}/recipes.
// Returns all recipes that produce the given fluid, as recipe cards — the
// fluid mirror of GetItemRecipesHandler (internal/api/items.go), sharing its
// buildRecipeCard.
func GetFluidRecipesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := r.PathValue("mod_id")
		fluidID := r.PathValue("fluid_id")
		if modID == "" || fluidID == "" {
			errBadRequest(w, "mod_id and fluid_id are required")
			return
		}
		rows, err := database.GetRecipesForFluid(r.Context(), modID, fluidID)
		if err != nil {
			errInternal(w, err)
			return
		}
		cards := make([]recipeCard, len(rows))
		for i, row := range rows {
			cards[i] = buildRecipeCard(row)
		}
		writeJSON(w, http.StatusOK, cards)
	}
}
