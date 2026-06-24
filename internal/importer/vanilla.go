package importer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// vanillaTypes are recipe types handled by VanillaParser.
var vanillaTypes = map[string]struct{}{
	"minecraft:smelting":                            {},
	"minecraft:blasting":                            {},
	"minecraft:smoking":                             {},
	"minecraft:campfire_cooking":                    {},
	"minecraft:crafting_shaped":                     {},
	"minecraft:crafting_shapeless":                  {},
	"minecraft:stonecutting":                        {},
	"minecraft:smithing_transform":                  {},
	"minecraft:crafting_transmute":                  {},
	"minecraft:crafting_imbue":                      {},
	"minecraft:crafting_dye":                        {},
	"minecraft:crafting_decorated_pot":              {},
	"minecraft:crafting_special_firework_rocket":    {},
	"minecraft:crafting_special_firework_star":      {},
	"minecraft:crafting_special_firework_star_fade": {},
	"minecraft:crafting_special_shielddecoration":   {},
	// Legacy pre-1.13 bare types (no namespace prefix)
	"crafting_shaped":    {},
	"crafting_shapeless": {},
	"smelting":           {},
}

// vanillaSkipTypes are vanilla recipe types that produce no useful optimizer data.
var vanillaSkipTypes = map[string]struct{}{
	"minecraft:smithing_trim":                    {}, // cosmetic, no result field
	"minecraft:crafting_special_bannerduplicate": {}, // implicit blank-banner input not in JSON
	"minecraft:crafting_special_repairitem":      {}, // non-deterministic durability merge
	"minecraft:crafting_special_mapextending":    {}, // dynamic map size
	"minecraft:crafting_special_mapcloning":      {}, // dynamic count
	"minecraft:crafting_special_bookcloning":     {}, // dynamic count
	"minecraft:crafting_special_armordye":        {}, // legacy name, replaced by crafting_dye
	"minecraft:crafting_special_tippedarrow":     {}, // legacy name, replaced by crafting_imbue
	"minecraft:crafting_special_suspiciousstew":                 {}, // stew effects not modeled
	"extended_industrialization:crafting_special_rainbowable_dye": {}, // no IO fields
}

func isVanillaType(t string) bool {
	_, ok := vanillaTypes[t]
	return ok
}

// IsVanillaSkip returns true for vanilla types that should be silently skipped.
func IsVanillaSkip(t string) bool {
	_, ok := vanillaSkipTypes[t]
	return ok
}

// VanillaParser handles vanilla Minecraft recipe formats.
type VanillaParser struct{}

func (p *VanillaParser) Skip(recipeType string) bool    { return IsVanillaSkip(recipeType) }
func (p *VanillaParser) Accepts(recipeType string) bool { return isVanillaType(recipeType) }

func (p *VanillaParser) KnownFields() []string {
	return []string{
		"type", "ingredient", "ingredients", "result", "experience", "cookingtime",
		"pattern", "key", "addition", "base", "template", "count",
		"shrubs", "dyes",
		"group", "category", "show_notification", "neoforge:conditions",
	}
}

func (p *VanillaParser) Decode(data []byte, modID, machineID, sourceFile string) (model.NormalizedRecipe, error) {
	var v vanillaRaw
	if err := json.Unmarshal(data, &v); err != nil {
		return model.NormalizedRecipe{}, fmt.Errorf("vanilla parse: %w", err)
	}

	norm := model.NormalizedRecipe{
		SourceFile: sourceFile,
		RecipeType: v.Type,
		ModID:      modID,
		MachineID:  machineID,
		EUPerTick:  0,
	}

	if err := fillVanilla(&v, &norm); err != nil {
		return model.NormalizedRecipe{}, err
	}

	norm.ContentHash = ContentHash(norm)
	return norm, nil
}

// fillVanilla populates norm's IO fields from a parsed vanillaRaw.
func fillVanilla(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	switch v.Type {
	case "minecraft:smelting", "minecraft:blasting", "minecraft:smoking", "minecraft:campfire_cooking",
		"smelting":
		return fillCooking(v, norm)
	case "minecraft:crafting_shaped", "crafting_shaped":
		return fillShaped(v, norm)
	case "minecraft:crafting_shapeless", "crafting_shapeless":
		return fillShapeless(v, norm)
	case "minecraft:stonecutting":
		return fillStonecutting(v, norm)
	case "minecraft:smithing_transform":
		return fillSmithingTransform(v, norm)
	case "minecraft:crafting_transmute":
		return fillCraftingTransmute(v, norm)
	case "minecraft:crafting_imbue":
		return fillImbue(v, norm)
	case "minecraft:crafting_dye":
		return fillDye(v, norm)
	case "minecraft:crafting_decorated_pot":
		return fillDecoratedPot(v, norm)
	case "minecraft:crafting_special_firework_rocket":
		return fillFireworkRocket(v, norm)
	case "minecraft:crafting_special_firework_star":
		return fillFireworkStar(v, norm)
	case "minecraft:crafting_special_firework_star_fade":
		return fillFireworkStarFade(v, norm)
	case "minecraft:crafting_special_shielddecoration":
		return fillShieldDecoration(v, norm)
	}
	return nil
}

// ── ingredient helpers ────────────────────────────────────────────────────────

// splitID splits "mod:id" into (mod, id). Bare IDs without ":" assume "minecraft" namespace.
func splitID(id string) (mod, item string) {
	if i := strings.IndexByte(id, ':'); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "minecraft", id
}

// ingToNormIO converts a vanilla ingredient + count to a NormalizedIO (prob always 1).
func ingToNormIO(ing vanillaIngredient, amount int) model.NormalizedIO {
	n := model.NormalizedIO{AmountNum: amount, AmountDen: 1, ProbNum: 1, ProbDen: 1}
	if ing.Tag != "" {
		t := ing.Tag
		n.TagName = &t
	} else {
		mod, item := splitID(ing.Item)
		n.ModID = &mod
		n.ID = &item
	}
	return n
}

// idToNormIO converts an item ID string + count to a NormalizedIO.
func idToNormIO(id string, amount int) model.NormalizedIO {
	mod, item := splitID(id)
	return model.NormalizedIO{
		AmountNum: amount, AmountDen: 1,
		ProbNum: 1, ProbDen: 1,
		ModID: &mod, ID: &item,
	}
}

// ── converters ───────────────────────────────────────────────────────────────

func fillCooking(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	ing := v.Ingredient.toIngredient()
	if ing.Item == "" && ing.Tag == "" {
		return fmt.Errorf("vanilla cooking %q: missing ingredient", v.Type)
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(ing, 1))

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla cooking %q: missing result", v.Type)
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))

	norm.Duration = v.CookingTime
	if norm.Duration <= 0 {
		switch v.Type {
		case "minecraft:blasting", "minecraft:smoking":
			norm.Duration = 100
		case "minecraft:campfire_cooking":
			norm.Duration = 600
		default: // smelting
			norm.Duration = 200
		}
	}
	return nil
}

func fillShaped(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	shape := make([]string, 9)
	for y, row := range v.Pattern {
		if y >= 3 {
			break
		}
		for x, ch := range row {
			if x >= 3 {
				break
			}
			if ch == ' ' {
				continue
			}
			ingVal, ok := v.Key[string(ch)]
			if !ok {
				continue
			}
			ing := ingVal.toIngredient()
			if ing.Tag != "" {
				shape[y*3+x] = "#" + ing.Tag
			} else {
				shape[y*3+x] = ing.Item
			}
		}
	}
	norm.Shape = shape

	type ioKey struct{ item, tag string }
	merged := make(map[ioKey]int)
	for _, cell := range shape {
		if cell == "" {
			continue
		}
		var k ioKey
		if strings.HasPrefix(cell, "#") {
			k.tag = cell[1:]
		} else {
			k.item = cell
		}
		merged[k]++
	}
	for k, amount := range merged {
		norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(vanillaIngredient{Item: k.item, Tag: k.tag}, amount))
	}

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_shaped: missing result")
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillShapeless(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	type ioKey struct{ item, tag string }
	merged := make(map[ioKey]int)
	for _, ingVal := range v.Ingredients {
		ing := ingVal.toIngredient()
		merged[ioKey{item: ing.Item, tag: ing.Tag}]++
	}
	for k, amount := range merged {
		norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(vanillaIngredient{Item: k.item, Tag: k.tag}, amount))
	}

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_shapeless: missing result")
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillStonecutting(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	ing := v.Ingredient.toIngredient()
	if ing.Item == "" && ing.Tag == "" {
		return fmt.Errorf("vanilla stonecutting: missing ingredient")
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(ing, 1))

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla stonecutting: missing result")
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillSmithingTransform(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	base := v.Base.toIngredient()
	if base.Item == "" && base.Tag == "" {
		return fmt.Errorf("vanilla smithing_transform: missing base")
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(base, 1))
	if !v.Addition.empty() {
		norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(v.Addition.toIngredient(), 1))
	}
	if !v.Template.empty() {
		norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(v.Template.toIngredient(), 1))
	}

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla smithing_transform: missing result")
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillCraftingTransmute(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	input := v.Input.toIngredient()
	if input.Item == "" && input.Tag == "" {
		return fmt.Errorf("vanilla crafting_transmute: missing input")
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(input, 1))
	if !v.Material.empty() {
		norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(v.Material.toIngredient(), 1))
	}

	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_transmute: missing result")
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillImbue(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	mat := v.Material.toIngredient()
	if mat.Item == "" && mat.Tag == "" {
		return fmt.Errorf("vanilla crafting_imbue: missing material")
	}
	src := v.Source.toIngredient()
	if src.Item == "" && src.Tag == "" {
		return fmt.Errorf("vanilla crafting_imbue: missing source")
	}
	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_imbue: missing result")
	}
	matCount := v.Result.count()
	if matCount <= 0 {
		matCount = 1
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(mat, matCount), ingToNormIO(src, 1))
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillDye(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	target := v.Target.toIngredient()
	if target.Item == "" && target.Tag == "" {
		return fmt.Errorf("vanilla crafting_dye: missing target")
	}
	dye := v.Dye.toIngredient()
	if dye.Item == "" && dye.Tag == "" {
		return fmt.Errorf("vanilla crafting_dye: missing dye")
	}
	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_dye: missing result")
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(target, 1), ingToNormIO(dye, 1))
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillDecoratedPot(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_decorated_pot: missing result")
	}
	type ioKey struct{ item, tag string }
	merged := make(map[ioKey]int)
	for _, side := range []vanillaIngredientValue{v.Back, v.Front, v.Left, v.Right} {
		ing := side.toIngredient()
		if ing.Item == "" && ing.Tag == "" {
			continue
		}
		merged[ioKey{item: ing.Item, tag: ing.Tag}]++
	}
	for k, count := range merged {
		norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(vanillaIngredient{Item: k.item, Tag: k.tag}, count))
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillFireworkRocket(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_special_firework_rocket: missing result")
	}
	type ioKey struct{ item, tag string }
	merged := make(map[ioKey]int)
	for _, iv := range []vanillaIngredientValue{v.Fuel, v.Shell, v.Star} {
		ing := iv.toIngredient()
		if ing.Item == "" && ing.Tag == "" {
			continue
		}
		merged[ioKey{item: ing.Item, tag: ing.Tag}]++
	}
	for k, count := range merged {
		norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(vanillaIngredient{Item: k.item, Tag: k.tag}, count))
	}
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillFireworkStar(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_special_firework_star: missing result")
	}
	fuel := v.Fuel.toIngredient()
	dye := v.Dye.toIngredient()
	if fuel.Item == "" && fuel.Tag == "" {
		return fmt.Errorf("vanilla crafting_special_firework_star: missing fuel")
	}
	if dye.Item == "" && dye.Tag == "" {
		return fmt.Errorf("vanilla crafting_special_firework_star: missing dye")
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(fuel, 1), ingToNormIO(dye, 1))
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillFireworkStarFade(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_special_firework_star_fade: missing result")
	}
	target := v.Target.toIngredient()
	dye := v.Dye.toIngredient()
	if target.Item == "" && target.Tag == "" {
		return fmt.Errorf("vanilla crafting_special_firework_star_fade: missing target")
	}
	if dye.Item == "" && dye.Tag == "" {
		return fmt.Errorf("vanilla crafting_special_firework_star_fade: missing dye")
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(target, 1), ingToNormIO(dye, 1))
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

func fillShieldDecoration(v *vanillaRaw, norm *model.NormalizedRecipe) error {
	id := v.Result.itemID()
	if id == "" {
		return fmt.Errorf("vanilla crafting_special_shielddecoration: missing result")
	}
	target := v.Target.toIngredient()
	banner := v.Banner.toIngredient()
	if target.Item == "" && target.Tag == "" {
		return fmt.Errorf("vanilla crafting_special_shielddecoration: missing target")
	}
	if banner.Item == "" && banner.Tag == "" {
		return fmt.Errorf("vanilla crafting_special_shielddecoration: missing banner")
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(target, 1), ingToNormIO(banner, 1))
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, v.Result.count()))
	norm.Duration = 1
	return nil
}

// ── raw JSON types ────────────────────────────────────────────────────────────

// vanillaIngredientValue handles both old object/array-of-object formats (pre-1.21)
// and the new string/array-of-string format (1.21+).
type vanillaIngredientValue struct {
	item string
	tag  string
}

func (vi *vanillaIngredientValue) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		vi.setFromString(s)
		return nil
	}
	if trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		if len(arr) == 0 {
			return nil
		}
		return vi.UnmarshalJSON(arr[0])
	}
	var obj struct {
		Item string `json:"item"`
		Tag  string `json:"tag"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	vi.item = obj.Item
	vi.tag = obj.Tag
	return nil
}

func (vi *vanillaIngredientValue) setFromString(s string) {
	if strings.HasPrefix(s, "#") {
		vi.tag = s[1:]
	} else {
		vi.item = s
	}
}

func (vi vanillaIngredientValue) toIngredient() vanillaIngredient {
	return vanillaIngredient{Item: vi.item, Tag: vi.tag}
}

func (vi vanillaIngredientValue) empty() bool {
	return vi.item == "" && vi.tag == ""
}

type vanillaIngredient struct {
	Item string
	Tag  string
}

type vanillaResult struct {
	str   string
	ID    string `json:"id"`
	Item  string `json:"item"`
	Count int    `json:"count"`
}

func (r *vanillaResult) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) > 0 && trimmed[0] == '"' {
		return json.Unmarshal(data, &r.str)
	}
	type alias vanillaResult
	return json.Unmarshal(data, (*alias)(r))
}

func (r vanillaResult) itemID() string {
	if r.str != "" {
		return r.str
	}
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
	Type        string                        `json:"type"`
	Ingredient  vanillaIngredientValue        `json:"ingredient"`
	CookingTime int                           `json:"cookingtime"`
	Result      vanillaResult                 `json:"result"`
	Pattern     []string                      `json:"pattern"`
	Key         map[string]vanillaIngredientValue `json:"key"`
	Ingredients []vanillaIngredientValue      `json:"ingredients"`
	Base        vanillaIngredientValue        `json:"base"`
	Addition    vanillaIngredientValue        `json:"addition"`
	Template    vanillaIngredientValue        `json:"template"`
	Input       vanillaIngredientValue        `json:"input"`
	Material    vanillaIngredientValue        `json:"material"`
	Source      vanillaIngredientValue        `json:"source"`
	Dye         vanillaIngredientValue        `json:"dye"`
	Target      vanillaIngredientValue        `json:"target"`
	Banner      vanillaIngredientValue        `json:"banner"`
	Fuel        vanillaIngredientValue        `json:"fuel"`
	Shell       vanillaIngredientValue        `json:"shell"`
	Star        vanillaIngredientValue        `json:"star"`
	Back        vanillaIngredientValue        `json:"back"`
	Front       vanillaIngredientValue        `json:"front"`
	Left        vanillaIngredientValue        `json:"left"`
	Right       vanillaIngredientValue        `json:"right"`
}
