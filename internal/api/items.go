package api

import (
	"context"
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/recipecard"
	"github.com/Wirezat/production-optimizer/internal/solver"
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
		cards, err := buildRecipeCards(r.Context(), database, rows)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, cards)
	}
}

// buildRecipeCards builds a recipe card per row and, where the row's machine
// has a usable slot layout, positions its I/O accordingly (recipecard.
// ApplySlotLayout) instead of leaving it to the frontend's generic grid.
// Slots are fetched once per distinct machine, not once per row.
func buildRecipeCards(ctx context.Context, database *db.DB, rows []*solver.RecipeRow) ([]recipecard.Card, error) {
	type machineKey struct{ modID, machineID string }
	slotsByMachine := map[machineKey][]*model.MachineSlot{}

	cards := make([]recipecard.Card, len(rows))
	for i, row := range rows {
		card := recipecard.Build(row)

		key := machineKey{row.MachineMod, row.MachineID}
		slots, cached := slotsByMachine[key]
		if !cached {
			var err error
			slots, err = database.ListMachineSlots(ctx, row.MachineMod, row.MachineID)
			if err != nil {
				return nil, err
			}
			slotsByMachine[key] = slots
		}
		recipecard.ApplySlotLayout(&card, slots)

		cards[i] = card
	}
	return cards, nil
}
