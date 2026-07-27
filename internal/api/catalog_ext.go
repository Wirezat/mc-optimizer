package api

import (
	"net/http"
	"strconv"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListMachineSlotsHandler returns slot layout for one machine.
func ListMachineSlotsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modID := r.PathValue("mod_id")
		machineID := r.PathValue("machine_id")
		slots, err := database.ListMachineSlots(r.Context(), modID, machineID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if slots == nil {
			slots = []*model.MachineSlot{}
		}
		writeJSON(w, http.StatusOK, slots)
	}
}

// SearchTagsHandler returns up to 50 tag names matching query q.
func SearchTagsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		offset := 0
		if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil {
			offset = v
		}
		tags, err := database.SearchTags(r.Context(), q, offset)
		if err != nil {
			errInternal(w, err)
			return
		}
		if tags == nil {
			tags = []string{}
		}
		writeJSON(w, http.StatusOK, tags)
	}
}

// ListTagMembersHandler returns every tag's members, grouped by tag name.
//
// Grouped rather than a flat list because that is the shape the caller needs:
// a recipe slot holding a tag has to show what the tag stands for, and the
// client already has every item's texture from the catalog.
func ListTagMembersHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		members, err := database.ListTagMembers(r.Context())
		if err != nil {
			errInternal(w, err)
			return
		}
		type ref struct {
			ModID  string `json:"mod_id"`
			ItemID string `json:"item_id"`
		}
		grouped := map[string][]ref{}
		for _, m := range members {
			grouped[m.TagName] = append(grouped[m.TagName], ref{m.ModID, m.ItemID})
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
