package api

import (
	"net/http"
	"strconv"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListTagMembersHandler returns every tag's members, grouped by kind, then by tag name.
func ListTagMembersHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		members, err := database.ListTagMembers(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		type ref struct {
			ModID string `json:"mod_id"`
			ID    string `json:"id"`
		}
		grouped := map[string]map[string][]ref{
			model.TagKindItem:  {},
			model.TagKindFluid: {},
		}
		for _, m := range members {
			grouped[m.Kind][m.TagName] = append(grouped[m.Kind][m.TagName], ref{m.ModID, m.ID})
		}
		writeJSON(w, http.StatusOK, grouped)
	}
}

// ListVillagerTradesHandler returns villager trades with resolved item names.
func ListVillagerTradesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profession := r.URL.Query().Get("profession")
		tierStr := r.URL.Query().Get("tier")
		tier := 0
		if tierStr != "" {
			if v, err := strconv.Atoi(tierStr); err == nil {
				tier = v
			}
		}
		trades, err := database.ListVillagerTrades(r.Context(), profession, tier)
		if err != nil {
			errInternal(w, err)
			return
		}
		if trades == nil {
			trades = []*model.VillagerTradeView{}
		}
		writeJSON(w, http.StatusOK, trades)
	}
}
