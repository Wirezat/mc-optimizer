package importer

import (
	"encoding/json"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// lootRaw is the top-level structure of a Minecraft loot table JSON.
type lootRaw struct {
	Type  string     `json:"type"`
	Pools []lootPool `json:"pools"`
}

type lootPool struct {
	Entries    []lootEntry       `json:"entries"`
	Conditions []json.RawMessage `json:"conditions"`
	Functions  []json.RawMessage `json:"functions"`
}

type lootEntry struct {
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Children   []lootEntry       `json:"children"`
	Functions  []json.RawMessage `json:"functions"`
	Conditions []json.RawMessage `json:"conditions"`
}

// ParseBlockLoot parses a block loot table and returns BlockDrop records.
// blockNS and blockID are the namespace and item ID of the block (from the file path).
func ParseBlockLoot(data []byte, blockNS, blockID string) []model.BlockDrop {
	var raw lootRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	var drops []model.BlockDrop
	for _, pool := range raw.Pools {
		poolCond := detectPoolCondition(pool.Conditions)
		for _, entry := range pool.Entries {
			drops = append(drops, extractLootDrops(blockNS, blockID, entry, poolCond)...)
		}
	}
	return drops
}

func extractLootDrops(blockNS, blockID string, entry lootEntry, inherited string) []model.BlockDrop {
	switch entry.Type {
	case "minecraft:item":
		if entry.Name == "" {
			return nil
		}
		parts := strings.SplitN(entry.Name, ":", 2)
		if len(parts) != 2 {
			return nil
		}
		cond := mergeCond(inherited, detectEntryCond(entry.Conditions, entry.Functions))
		min, max := extractCountRange(entry.Functions)
		return []model.BlockDrop{{
			BlockModID:  blockNS,
			BlockItemID: blockID,
			DropModID:   parts[0],
			DropItemID:  parts[1],
			MinCount:    min,
			MaxCount:    max,
			Condition:   cond,
		}}

	case "minecraft:alternatives":
		// Each child in alternatives is a separate conditional path.
		// Children inherit parent conditions but may add their own.
		parentCond := mergeCond(inherited, detectEntryCond(entry.Conditions, entry.Functions))
		var drops []model.BlockDrop
		for _, child := range entry.Children {
			drops = append(drops, extractLootDrops(blockNS, blockID, child, parentCond)...)
		}
		return drops

	case "minecraft:sequence", "minecraft:group":
		parentCond := mergeCond(inherited, detectEntryCond(entry.Conditions, entry.Functions))
		var drops []model.BlockDrop
		for _, child := range entry.Children {
			drops = append(drops, extractLootDrops(blockNS, blockID, child, parentCond)...)
		}
		return drops
	}
	return nil
}

// detectPoolCondition checks pool-level conditions for silk_touch or fortune markers.
func detectPoolCondition(conds []json.RawMessage) string {
	return rawCondStr(conds, nil)
}

// detectEntryCond checks entry-level conditions and functions for silk_touch/fortune markers.
func detectEntryCond(conds []json.RawMessage, fns []json.RawMessage) string {
	if c := rawCondStr(conds, nil); c != "" {
		return c
	}
	return rawCondStr(nil, fns)
}

func rawCondStr(conds []json.RawMessage, fns []json.RawMessage) string {
	hasSilk := false
	hasFortune := false

	check := func(raw json.RawMessage) {
		s := strings.ToLower(string(raw))
		if strings.Contains(s, "silk_touch") {
			hasSilk = true
		}
		if strings.Contains(s, "table_bonus") || strings.Contains(s, "ore_drops") || strings.Contains(s, "apply_bonus") {
			hasFortune = true
		}
	}

	for _, c := range conds {
		check(c)
	}
	for _, f := range fns {
		check(f)
	}

	if hasSilk {
		return "silk_touch"
	}
	if hasFortune {
		return "fortune"
	}
	return ""
}

func mergeCond(inherited, own string) string {
	if own != "" {
		return own
	}
	if inherited != "" {
		return inherited
	}
	return "normal"
}

// extractCountRange reads min/max counts from loot functions.
// Looks for minecraft:set_count with a uniform_number_provider or plain int.
func extractCountRange(fns []json.RawMessage) (min, max int) {
	min, max = 1, 1
	for _, fn := range fns {
		var f struct {
			Function string          `json:"function"`
			Count    json.RawMessage `json:"count"`
		}
		if err := json.Unmarshal(fn, &f); err != nil {
			continue
		}
		fname := f.Function
		if fname != "minecraft:set_count" && fname != "set_count" {
			continue
		}
		if len(f.Count) == 0 {
			continue
		}
		// Try plain int.
		var n int
		if err := json.Unmarshal(f.Count, &n); err == nil {
			min, max = n, n
			return
		}
		// Try {min, max} or {type, min, max}.
		var rng struct {
			Min json.RawMessage `json:"min"`
			Max json.RawMessage `json:"max"`
		}
		if err := json.Unmarshal(f.Count, &rng); err == nil {
			if v, err := parseIntOrObj(rng.Min); err == nil {
				min = v
			}
			if v, err := parseIntOrObj(rng.Max); err == nil {
				max = v
			}
		}
		return
	}
	return
}

func parseIntOrObj(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f), nil
	}
	return 0, errNotInt
}

var errNotInt = &parseError{"not int"}

type parseError struct{ msg string }

func (e *parseError) Error() string { return e.msg }
