package importer

// resolved holds a canonical mod and machine for a recipe type.
type resolved struct {
	modID     string
	machineID string
}

// knownMappings maps a recipe type to ALL machines that consume it.
// A single recipe type (e.g. minecraft:smelting) can belong to several
// machines across mods, since every mod that supports vanilla smelting
// inherits those recipes. The importer emits one recipe per entry.
var knownMappings = map[string][]resolved{
	// Legacy pre-1.13 bare types (no minecraft: prefix)
	"crafting_shaped":    {{modID: "minecraft", machineID: "crafting_table"}},
	"crafting_shapeless": {{modID: "minecraft", machineID: "crafting_table"}},
	"smelting": {
		{modID: "minecraft", machineID: "furnace"},
		{modID: "modern_industrialization", machineID: "furnace"},
		{modID: "extended_industrialization", machineID: "large_electric_furnace"},
		{modID: "extended_industrialization", machineID: "large_steam_furnace"},
	},
	// Namespaced types
	"minecraft:smelting": {
		{modID: "minecraft", machineID: "furnace"},
		{modID: "modern_industrialization", machineID: "furnace"},
		{modID: "extended_industrialization", machineID: "large_electric_furnace"},
		{modID: "extended_industrialization", machineID: "large_steam_furnace"},
	},
	"minecraft:blasting": {
		{modID: "minecraft", machineID: "blast_furnace"},
		{modID: "modern_industrialization", machineID: "furnace"},
		{modID: "extended_industrialization", machineID: "large_electric_furnace"},
		{modID: "extended_industrialization", machineID: "large_steam_furnace"},
	},
	"minecraft:smoking": {
		{modID: "minecraft", machineID: "smoker"},
		{modID: "modern_industrialization", machineID: "furnace"},
		{modID: "extended_industrialization", machineID: "large_electric_furnace"},
		{modID: "extended_industrialization", machineID: "large_steam_furnace"},
	},
	// The Industrialization Overdrive pyrolyse oven is an upgrade of the MI coke
	// oven and shares its recipes (coal → coke). Only emitted when Overdrive is
	// present (guarded by presentMods in the importer).
	"modern_industrialization:coke_oven": {
		{modID: "modern_industrialization", machineID: "coke_oven"},
		{modID: "industrialization_overdrive", machineID: "pyrolyse_oven"},
	},
	// Extended Industrialization large macerators are batching multiblocks that
	// run the MI macerator recipe type (MIMachineRecipeTypes.MACERATOR). Likewise
	// its large furnaces run MIMachineRecipeTypes.FURNACE — handled in the
	// smelting/blasting/smoking entries above. presentMods-guarded.
	"modern_industrialization:macerator": {
		{modID: "modern_industrialization", machineID: "macerator"},
		{modID: "extended_industrialization", machineID: "large_electric_macerator"},
		{modID: "extended_industrialization", machineID: "large_steam_macerator"},
	},
	"minecraft:campfire_cooking":                    {{modID: "minecraft", machineID: "campfire"}},
	"minecraft:crafting_shaped":                     {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_shapeless":                  {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:stonecutting":                        {{modID: "minecraft", machineID: "stonecutter"}},
	"minecraft:smithing_transform":                  {{modID: "minecraft", machineID: "smithing_table"}},
	"minecraft:crafting_transmute":                  {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_imbue":                      {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_dye":                        {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_decorated_pot":              {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_special_firework_rocket":    {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_special_firework_star":      {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_special_firework_star_fade": {{modID: "minecraft", machineID: "crafting_table"}},
	"minecraft:crafting_special_shielddecoration":   {{modID: "minecraft", machineID: "crafting_table"}},
}

// ResolveRecipeType maps a recipe type string to all (modID, machineID) pairs
// that consume it. Checks hardcoded mappings first, then falls back to
// auto-deriving a single pair from the "mod:type" string.
func ResolveRecipeType(recipeType string) (mappings []resolved, ok bool) {
	if r, found := knownMappings[recipeType]; found {
		return r, true
	}
	mod, machine, err := SplitTypeField(recipeType)
	if err != nil {
		return nil, false
	}
	return []resolved{{modID: mod, machineID: machine}}, true
}
