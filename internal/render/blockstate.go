package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// blockState is the subset of Minecraft's blockstate format this renderer
// resolves, used as a fallback when a block-item ships no models/item/<id>.json.
// Placement state (facing, powered, ...) is not evaluated: variants/multipart
// entries are picked deterministically instead (see resolveBlockState).
type blockState struct {
	Variants  map[string]variantEntry `json:"variants"`
	Multipart []multipartEntry        `json:"multipart"`
}

// multipartEntry is one layer of a multipart blockstate. Only Apply.Model is read.
type multipartEntry struct {
	Apply variantEntry `json:"apply"`
}

// variantEntry is a variant's model choice. A JSON list (vanilla's random
// visual variety) collapses to its first element.
type variantEntry struct {
	Model string
}

func (v *variantEntry) UnmarshalJSON(data []byte) error {
	var one struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(data, &one); err == nil && one.Model != "" {
		v.Model = one.Model
		return nil
	}
	var many []struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(data, &many); err != nil || len(many) == 0 {
		return fmt.Errorf("render: blockstate variant has no model")
	}
	v.Model = many[0].Model
	return nil
}

// itemDefinition is vanilla's per-item model file (assets/<ns>/items/<id>.json,
// 1.21.2+). Only its "minecraft:model" entry type (a bare model reference) is
// read; other entry types (composite, select, condition, ...) are left unresolved.
type itemDefinition struct {
	Model itemModelEntry `json:"model"`
}

type itemModelEntry struct {
	Type  string `json:"type"`
	Model string `json:"model"`
}

// resolveItemDefinition returns modID/itemID's model ref from its
// items/<id>.json, or ok=false if the file is missing or not a bare
// "minecraft:model" entry.
func (l *Loader) resolveItemDefinition(modID, itemID string) (ref string, ok bool) {
	path := filepath.Join(l.assetsDir, modID, "items", itemID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var def itemDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		return "", false
	}
	if def.Model.Type != "minecraft:model" || def.Model.Model == "" {
		return "", false
	}
	return def.Model.Model, true
}

// resolveBlockState returns modID/blockID's model ref from its blockstate, or
// ok=false if the file is missing, malformed, or has no usable variant/multipart
// entry. The "" variant wins if present, else the alphabetically-first key,
// else a multipart blockstate's first entry — a deterministic, real model.
func (l *Loader) resolveBlockState(modID, blockID string) (ref string, ok bool) {
	path := filepath.Join(l.assetsDir, modID, "blockstates", blockID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var bs blockState
	if err := json.Unmarshal(data, &bs); err != nil {
		return "", false
	}
	if len(bs.Variants) == 0 {
		if len(bs.Multipart) == 0 {
			return "", false
		}
		return bs.Multipart[0].Apply.Model, true
	}
	if v, present := bs.Variants[""]; present {
		return v.Model, true
	}
	keys := make([]string, 0, len(bs.Variants))
	for k := range bs.Variants {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return bs.Variants[keys[0]].Model, true
}
