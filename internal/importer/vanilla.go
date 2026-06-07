package importer

import (
	"encoding/json"
	"fmt"
	"strings"
)

var vanillaTypes = map[string]struct{}{
	"minecraft:smelting":           {},
	"minecraft:blasting":           {},
	"minecraft:smoking":            {},
	"minecraft:campfire_cooking":   {},
	"minecraft:crafting_shaped":    {},
	"minecraft:crafting_shapeless": {},
}

func isVanillaType(t string) bool {
	_, ok := vanillaTypes[t]
	return ok
}

// vanillaIngredient is one slot ingredient: either item or tag reference.
type vanillaIngredient struct {
	Item string `json:"item"`
	Tag  string `json:"tag"`
}

// vanillaIngredientValue unmarshals both a single ingredient object and an array
// (Minecraft allows arrays to express alternative ingredients for one slot).
// We always use the first element.
type vanillaIngredientValue struct {
	v []vanillaIngredient
}

func (vi *vanillaIngredientValue) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '[' {
		var arr []vanillaIngredient
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		vi.v = arr
		return nil
	}
	var obj vanillaIngredient
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	vi.v = []vanillaIngredient{obj}
	return nil
}

func (vi vanillaIngredientValue) first() (vanillaIngredient, bool) {
	if len(vi.v) == 0 {
		return vanillaIngredient{}, false
	}
	return vi.v[0], true
}

type vanillaResult struct {
	ID    string `json:"id"`   // 1.20.x+
	Item  string `json:"item"` // legacy
	Count int    `json:"count"`
}

func (r vanillaResult) itemID() string {
	if r.ID != "" {
		return r.ID
	}
	return r.Item
}

func (r vanillaResult) count() int {
	if r.Count <= 0 {
		return 1
	}
	return r.Count
}

type vanillaRaw struct {
	Type string `json:"type"`
	// Cooking (smelting / blasting / smoking / campfire_cooking)
	Ingredient  vanillaIngredientValue            `json:"ingredient"`
	CookingTime int                               `json:"cookingtime"`
	Result      vanillaResult                     `json:"result"`
	// Shaped crafting
	Pattern []string                              `json:"pattern"`
	Key     map[string]vanillaIngredientValue     `json:"key"`
	// Shapeless crafting
	Ingredients []vanillaIngredientValue           `json:"ingredients"`
}

// convertVanillaInPlace fills rec's IO and timing fields from the vanilla-format JSON.
// Called when rec.Type is a known vanilla recipe type.
func convertVanillaInPlace(data []byte, rec *MIRecipe) error {
	var v vanillaRaw
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("vanilla parse: %w", err)
	}
	switch v.Type {
	case "minecraft:smelting", "minecraft:blasting", "minecraft:smoking", "minecraft:campfire_cooking":
		return convertCooking(&v, rec)
	case "minecraft:crafting_shaped":
		return convertShaped(&v, rec)
	case "minecraft:crafting_shapeless":
		return convertShapeless(&v, rec)
	}
	return nil
}

func convertCooking(v *vanillaRaw, rec *MIRecipe) error {
	ing, ok := v.Ingredient.first()
	if !ok {
		return fmt.Errorf("vanilla cooking %q: missing ingredient", v.Type)
	}
	rec.ItemInputs = RawIOList{ingToRawIO(ing, 1)}

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla cooking %q: missing result", v.Type)
	}
	if !strings.Contains(id, ":") {
		return fmt.Errorf("vanilla cooking %q: invalid result id %q", v.Type, id)
	}
	rec.ItemOutputs = RawIOList{{Item: id, Amount: v.Result.count(), Probability: 1.0}}

	rec.Duration = v.CookingTime
	if rec.Duration <= 0 {
		switch v.Type {
		case "minecraft:blasting", "minecraft:smoking":
			rec.Duration = 100
		default:
			rec.Duration = 200
		}
	}
	rec.EU = 0
	return nil
}

func convertShaped(v *vanillaRaw, rec *MIRecipe) error {
	// Count how many times each key char appears in the pattern.
	counts := make(map[string]int)
	for _, row := range v.Pattern {
		for _, ch := range row {
			if ch != ' ' {
				counts[string(ch)]++
			}
		}
	}

	// Merge inputs with same item/tag (same key char or different chars pointing to same thing).
	type ioKey struct{ item, tag string }
	merged := make(map[ioKey]int)
	for ch, n := range counts {
		ingVal, ok := v.Key[ch]
		if !ok {
			continue
		}
		ing, ok := ingVal.first()
		if !ok {
			continue
		}
		k := ioKey{item: ing.Item, tag: ing.Tag}
		merged[k] += n
	}
	for k, amount := range merged {
		rec.ItemInputs = append(rec.ItemInputs, ingToRawIO(vanillaIngredient{Item: k.item, Tag: k.tag}, amount))
	}

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_shaped: missing result")
	}
	rec.ItemOutputs = RawIOList{{Item: id, Amount: v.Result.count(), Probability: 1.0}}
	rec.Duration = 1
	rec.EU = 0
	return nil
}

func convertShapeless(v *vanillaRaw, rec *MIRecipe) error {
	type ioKey struct{ item, tag string }
	merged := make(map[ioKey]int)
	for _, ingVal := range v.Ingredients {
		ing, ok := ingVal.first()
		if !ok {
			continue
		}
		k := ioKey{item: ing.Item, tag: ing.Tag}
		merged[k]++
	}
	for k, amount := range merged {
		rec.ItemInputs = append(rec.ItemInputs, ingToRawIO(vanillaIngredient{Item: k.item, Tag: k.tag}, amount))
	}

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_shapeless: missing result")
	}
	rec.ItemOutputs = RawIOList{{Item: id, Amount: v.Result.count(), Probability: 1.0}}
	rec.Duration = 1
	rec.EU = 0
	return nil
}

func ingToRawIO(ing vanillaIngredient, amount int) RawIO {
	io := RawIO{Amount: amount, Probability: 1.0}
	if ing.Tag != "" {
		io.Tag = ing.Tag
	} else {
		io.Item = ing.Item
	}
	return io
}
