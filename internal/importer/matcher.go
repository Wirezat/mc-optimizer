package importer

// resolved holds the canonical mod and machine for a recipe type.
type resolved struct {
	modID     string
	machineID string
}

// vanillaMachines maps built-in vanilla recipe types to their canonical machine.
var vanillaMachines = map[string]resolved{
	// Legacy pre-1.13 bare types (no minecraft: prefix)
	"crafting_shaped":    {modID: "minecraft", machineID: "crafting_table"},
	"crafting_shapeless": {modID: "minecraft", machineID: "crafting_table"},
	"smelting":           {modID: "minecraft", machineID: "furnace"},
	// Namespaced types
	"minecraft:smelting":                            {modID: "minecraft", machineID: "furnace"},
	"minecraft:blasting":                            {modID: "minecraft", machineID: "blast_furnace"},
	"minecraft:smoking":                             {modID: "minecraft", machineID: "smoker"},
	"minecraft:campfire_cooking":                    {modID: "minecraft", machineID: "campfire"},
	"minecraft:crafting_shaped":                     {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_shapeless":                  {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:stonecutting":                        {modID: "minecraft", machineID: "stonecutter"},
	"minecraft:smithing_transform":                  {modID: "minecraft", machineID: "smithing_table"},
	"minecraft:crafting_transmute":                  {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_imbue":                      {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_dye":                        {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_decorated_pot":              {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_firework_rocket":    {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_firework_star":      {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_firework_star_fade": {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_shielddecoration":   {modID: "minecraft", machineID: "crafting_table"},
}

// ResolveRecipeType maps a recipe type string to (modID, machineID).
// Checks hardcoded vanilla mappings first, then auto-derives from the "mod:type" string.
func ResolveRecipeType(recipeType string) (modID, machineID string, ok bool) {
	if r, found := vanillaMachines[recipeType]; found {
		return r.modID, r.machineID, true
	}
	mod, machine, err := SplitTypeField(recipeType)
	if err != nil {
		return "", "", false
	}
	return mod, machine, true
}
