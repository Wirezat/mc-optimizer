package api

import (
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/recipecard"
)

// GetItemRecipesHandler handles GET /api/items/{mod_id}/{item_id}/recipes.
// Returns all recipes that produce the given item, as recipe cards.
func GetItemRecipesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := r.PathValue("mod_id")
		itemID := r.PathValue("item_id")
		if modID == "" || itemID == "" {
			errBadRequest(w, "mod_id and item_id are required")
			return
		}
		rows, err := database.GetRecipesForItem(r.Context(), modID, itemID)
		if err != nil {
			errInternal(w, err)
			return
		}
		cards := make([]recipecard.Card, len(rows))
		for i, row := range rows {
			cards[i] = recipecard.Build(row)
		}
		writeJSON(w, http.StatusOK, cards)
	}
}
