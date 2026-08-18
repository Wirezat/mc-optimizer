package api

import (
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// recipeCard is the JSON shape both the item and fluid recipe-detail
// endpoints return: enough to render a crafting-grid card, deliberately
// carrying no display names (the frontend already has those cached).
type recipeCard struct {
	ID            string             `json:"id"`
	MachineModID  string             `json:"machine_mod_id"`
	MachineID     string             `json:"machine_id"`
	DurationTicks int                `json:"duration_ticks"`
	EUPerTick     int64              `json:"eu_per_tick"`
	TotalEU       int64              `json:"total_eu"`
	Inputs        []recipeCardInput  `json:"inputs"`
	Outputs       []recipeCardOutput `json:"outputs"`
	FluidInputs   []recipeCardFluid  `json:"fluid_inputs"`
	FluidOutputs  []recipeCardFluid  `json:"fluid_outputs"`
}

// recipeCardInput is a concrete item (item_mod_id+item_id set, tag_name
// empty) or a tag slot (tag_name set, item_mod_id/item_id empty) — never both.
type recipeCardInput struct {
	ItemModID    string  `json:"item_mod_id,omitempty"`
	ItemID       string  `json:"item_id,omitempty"`
	TagName      string  `json:"tag_name,omitempty"`
	Amount       float64 `json:"amount"`
	NonConsuming bool    `json:"non_consuming,omitempty"`
}

type recipeCardOutput struct {
	ItemModID string  `json:"item_mod_id"`
	ItemID    string  `json:"item_id"`
	Amount    float64 `json:"amount"`
}

type recipeCardFluid struct {
	FluidModID string `json:"fluid_mod_id"`
	FluidID    string `json:"fluid_id"`
	AmountMB   int64  `json:"amount_mb"`
}

// buildRecipeCard converts one solver.RecipeRow into the recipe-card JSON
// shape. Pure function — no I/O — so it's tested directly without a DB.
func buildRecipeCard(r *solver.RecipeRow) recipeCard {
	card := recipeCard{
		ID: r.ID, MachineModID: r.MachineMod, MachineID: r.MachineID,
		DurationTicks: r.DurationTicks, EUPerTick: r.EUPerTick, TotalEU: r.TotalEU,
		Inputs:       make([]recipeCardInput, 0, len(r.ItemInputs)),
		Outputs:      make([]recipeCardOutput, 0, len(r.ItemOutputs)),
		FluidInputs:  make([]recipeCardFluid, 0, len(r.FluidInputs)),
		FluidOutputs: make([]recipeCardFluid, 0, len(r.FluidOutputs)),
	}
	for _, in := range r.ItemInputs {
		v := recipeCardInput{
			Amount:       float64(in.AmountNum) / float64(in.AmountDen),
			NonConsuming: in.NonConsuming,
		}
		if in.TagName != nil {
			v.TagName = *in.TagName
		} else {
			if in.ItemModID != nil {
				v.ItemModID = *in.ItemModID
			}
			if in.ItemID != nil {
				v.ItemID = *in.ItemID
			}
		}
		card.Inputs = append(card.Inputs, v)
	}
	for _, o := range r.ItemOutputs {
		v := recipeCardOutput{Amount: float64(o.AmountNum) / float64(o.AmountDen)}
		if o.ItemModID != nil {
			v.ItemModID = *o.ItemModID
		}
		if o.ItemID != nil {
			v.ItemID = *o.ItemID
		}
		card.Outputs = append(card.Outputs, v)
	}
	for _, fi := range r.FluidInputs {
		card.FluidInputs = append(card.FluidInputs, recipeCardFluid{FluidModID: fi.FluidModID, FluidID: fi.FluidID, AmountMB: fi.AmountMB})
	}
	for _, fo := range r.FluidOutputs {
		card.FluidOutputs = append(card.FluidOutputs, recipeCardFluid{FluidModID: fo.FluidModID, FluidID: fo.FluidID, AmountMB: fo.AmountMB})
	}
	return card
}

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
		cards := make([]recipeCard, len(rows))
		for i, row := range rows {
			cards[i] = buildRecipeCard(row)
		}
		writeJSON(w, http.StatusOK, cards)
	}
}
