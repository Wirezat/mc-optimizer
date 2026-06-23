package importer

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// ParseVillagerTrade parses a villager trade JSON file.
// relPath is the path within the villager_trade/ category, e.g.
//
//	"armorer/1/sell_iron.json"      → profession=armorer, tier=1
//	"wandering_trader/buy_map.json" → profession=wandering_trader, tier=1
//
// MC 26.x format: each file is a single trade object.
// MC older / modded format: file may have a "trades": [...] array wrapper.
func ParseVillagerTrade(data []byte, relPath string) []model.VillagerTrade {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	// Strip .json from last segment.
	parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], ".json")

	var profession string
	var tier int
	switch len(parts) {
	case 2:
		// wandering_trader/<trade>  → tier 1
		profession = parts[0]
		tier = 1
	case 3:
		// <profession>/<tier>/<trade>
		profession = parts[0]
		if t, err := strconv.Atoi(parts[1]); err == nil && t >= 1 && t <= 5 {
			tier = t
		} else {
			tier = 1
		}
	default:
		return nil
	}

	// Try array-wrapper format first.
	var wrapper struct {
		Trades []villagerOffer26 `json:"trades"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper.Trades) > 0 {
		var result []model.VillagerTrade
		for _, o := range wrapper.Trades {
			if t := o.toModel(profession, tier); t != nil {
				result = append(result, *t)
			}
		}
		return result
	}

	// Single trade object (MC 26.x default).
	var single villagerOffer26
	if err := json.Unmarshal(data, &single); err != nil {
		return nil
	}
	if t := single.toModel(profession, tier); t != nil {
		return []model.VillagerTrade{*t}
	}
	return nil
}

// villagerOffer26 handles the MC 26.x villager trade offer JSON.
// Notable differences from older format:
//   - "wants" can be a single object OR an array of objects.
//   - "count" fields are floats (e.g. 5.0) not ints.
//   - "gives" is a single object (not array).
type villagerOffer26 struct {
	Wants                villagerWants   `json:"wants"`
	Gives                villagerItem26  `json:"gives"`
	MaxUses              *float64        `json:"max_uses"`
	XP                   *float64        `json:"xp"`
	GivenItemModifiers   json.RawMessage `json:"given_item_modifiers"`
}

// villagerWants unmarshals "wants" as either a single item or an array.
type villagerWants struct {
	Items []villagerItem26
}

func (w *villagerWants) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '[' {
		return json.Unmarshal(data, &w.Items)
	}
	var single villagerItem26
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	w.Items = []villagerItem26{single}
	return nil
}

type villagerItem26 struct {
	ID    string  `json:"id"`
	Count float64 `json:"count"`
}

func (o villagerOffer26) toModel(profession string, tier int) *model.VillagerTrade {
	giveParts := strings.SplitN(o.Gives.ID, ":", 2)
	if len(giveParts) != 2 || giveParts[0] == "" || giveParts[1] == "" {
		return nil
	}
	if len(o.Wants.Items) == 0 {
		return nil
	}
	want := o.Wants.Items[0]
	costParts := strings.SplitN(want.ID, ":", 2)
	if len(costParts) != 2 || costParts[0] == "" || costParts[1] == "" {
		return nil
	}

	giveCount := int(o.Gives.Count)
	if giveCount <= 0 {
		giveCount = 1
	}
	costCount := int(want.Count)
	if costCount <= 0 {
		costCount = 1
	}

	modified := len(o.GivenItemModifiers) > 0 && string(o.GivenItemModifiers) != "null" && string(o.GivenItemModifiers) != "[]"

	var maxUses *int
	if o.MaxUses != nil {
		v := int(*o.MaxUses)
		maxUses = &v
	}
	var xp *int
	if o.XP != nil {
		v := int(*o.XP)
		xp = &v
	}

	return &model.VillagerTrade{
		Profession:     profession,
		Tier:           tier,
		CostModID:      costParts[0],
		CostItemID:     costParts[1],
		CostCount:      costCount,
		ResultModID:    giveParts[0],
		ResultItemID:   giveParts[1],
		ResultCount:    giveCount,
		ResultModified: modified,
		MaxUses:        maxUses,
		XP:             xp,
	}
}
