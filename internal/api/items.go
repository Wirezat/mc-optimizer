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
		cards, err := buildRecipeCards(r.Context(), database, dedupeByRecipeID(rows))
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, cards)
	}
}

// dedupeByRecipeID keeps the first row per recipe id. GetRecipesForItem and
// GetRecipesForFluid yield one row per candidate machine so the solver's chain
// can offer them all, but a catalog page answers "what recipes exist" rather
// than "which machine to build" — so it collapses back to one card per recipe.
// The query orders each recipe's own machine first, so the kept row is always
// that recipe's base machine.
func dedupeByRecipeID(rows []*solver.RecipeRow) []*solver.RecipeRow {
	seen := make(map[string]bool, len(rows))
	out := make([]*solver.RecipeRow, 0, len(rows))
	for _, row := range rows {
		if seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		out = append(out, row)
	}
	return out
}

// buildRecipeCards builds a recipe card per row, applying each row's
// machine's slot layout (recipecard.ApplySlotLayout). Slots are fetched once
// per distinct machine.
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
