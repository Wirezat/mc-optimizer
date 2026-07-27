package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// blockState is the subset of Minecraft's blockstate format this renderer
// resolves. It exists only as a fallback: every item ships its own
// models/item/<id>.json pointing at the right visual, so this is reached
// only when a mod's block-item omits one.
//
// Not implemented: `multipart`, and a variant's `x`/`y` rotation. Both exist
// to pick a look for a block's placement state (facing, powered, connected
// neighbours, ...) — state a placed block has and an inventory icon does
// not. There is no single correct choice to render for those, so a
// blockstate that needs one is left unresolved rather than guessed at.
type blockState struct {
	Variants map[string]variantEntry `json:"variants"`
}

// variantEntry is a variant's model choice. Vanilla allows a list here for
// random visual variety between equally-valid options; an icon wants one
// deterministic answer, so only the first is kept.
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

// resolveBlockState picks a model reference for blockID's blockstate, or
// ok=false if the file is missing, malformed, or has no variant this
// renderer can pick without knowing the block's placement state.
//
// A variant keyed "" is the no-properties case (most ordinary blocks: a
// stone, a bookshelf) and always wins outright. Otherwise the
// alphabetically-first key is used, so the result is stable across calls —
// not necessarily the state a player would picture, but a real model rather
// than none.
func (l *Loader) resolveBlockState(modID, blockID string) (ref string, ok bool) {
	path := filepath.Join(l.assetsDir, modID, "blockstates", blockID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var bs blockState
	if err := json.Unmarshal(data, &bs); err != nil || len(bs.Variants) == 0 {
		return "", false
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
