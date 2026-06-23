package api

import (
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// GetItemRecipesHandler handles GET /api/items/{mod_id}/{item_id}/recipes.
// Returns all recipes that produce the given item, with full IO details.
func GetItemRecipesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID  := r.PathValue("mod_id")
		itemID := r.PathValue("item_id")
		if modID == "" || itemID == "" {
			errBadRequest(w, "mod_id and item_id are required")
			return
		}
		recipes, err := database.GetRecipesForItem(r.Context(), modID, itemID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if recipes == nil {
			recipes = []*solver.RecipeRow{}
		}
		type inputView struct {
			ItemModID string  `json:"item_mod_id,omitempty"`
			ItemID    string  `json:"item_id,omitempty"`
			TagID     string  `json:"tag_id,omitempty"`
			Amount    float64 `json:"amount"`
		}
		type outputView struct {
			ItemModID string  `json:"item_mod_id"`
			ItemID    string  `json:"item_id"`
			Amount    float64 `json:"amount"`
		}
		type fluidView struct {
			ModID    string `json:"mod_id"`
			FluidID  string `json:"fluid_id"`
			AmountMB int64  `json:"amount_mb"`
		}
		type recipeView struct {
			ID            string       `json:"id"`
			MachineMod    string       `json:"machine_mod"`
			MachineID     string       `json:"machine_id"`
			DurationTicks int          `json:"duration_ticks"`
			Inputs        []inputView  `json:"inputs"`
			Outputs       []outputView `json:"outputs"`
			FluidInputs   []fluidView  `json:"fluid_inputs"`
			FluidOutputs  []fluidView  `json:"fluid_outputs"`
		}
		out := make([]recipeView, len(recipes))
		for i, r := range recipes {
			rv := recipeView{
				ID: r.ID, MachineMod: r.MachineMod, MachineID: r.MachineID,
				DurationTicks: r.DurationTicks,
			}
			for _, in := range r.ItemInputs {
				v := inputView{Amount: float64(in.AmountNum) / float64(in.AmountDen)}
				if in.ItemModID != nil { v.ItemModID = *in.ItemModID }
				if in.ItemID    != nil { v.ItemID    = *in.ItemID }
				if in.TagID     != nil { v.TagID     = *in.TagID }
				rv.Inputs = append(rv.Inputs, v)
			}
			for _, o := range r.ItemOutputs {
				var ov outputView
				if o.ItemModID != nil { ov.ItemModID = *o.ItemModID }
				if o.ItemID    != nil { ov.ItemID    = *o.ItemID }
				ov.Amount = float64(o.AmountNum) / float64(o.AmountDen)
				rv.Outputs = append(rv.Outputs, ov)
			}
			for _, fi := range r.FluidInputs {
				rv.FluidInputs = append(rv.FluidInputs, fluidView{ModID: fi.FluidModID, FluidID: fi.FluidID, AmountMB: fi.AmountMB})
			}
			for _, fo := range r.FluidOutputs {
				rv.FluidOutputs = append(rv.FluidOutputs, fluidView{ModID: fo.FluidModID, FluidID: fo.FluidID, AmountMB: fo.AmountMB})
			}
			if rv.Inputs == nil       { rv.Inputs = []inputView{} }
			if rv.Outputs == nil      { rv.Outputs = []outputView{} }
			if rv.FluidInputs == nil  { rv.FluidInputs = []fluidView{} }
			if rv.FluidOutputs == nil { rv.FluidOutputs = []fluidView{} }
			out[i] = rv
		}
		writeJSON(w, http.StatusOK, out)
	}
}
