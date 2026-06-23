package importer

import (
	"regexp"

	"github.com/Wirezat/production-optimizer/internal/model"
)

type matcher struct {
	exact   map[string]resolved
	regexes []regexEntry
}

type resolved struct {
	modID     string
	machineID string
}

type regexEntry struct {
	re            *regexp.Regexp
	targetMod     string
	targetMachine string
}

func buildMatcher(vrts []*model.ValidRecipeType) matcher {
	m := matcher{exact: make(map[string]resolved)}
	for _, v := range vrts {
		var tMod, tMachine string
		if v.TargetModID != nil {
			tMod = *v.TargetModID
		}
		if v.TargetMachineID != nil {
			tMachine = *v.TargetMachineID
		}
		if v.IsRegex {
			re, err := regexp.Compile(v.Pattern)
			if err != nil {
				continue
			}
			m.regexes = append(m.regexes, regexEntry{re, tMod, tMachine})
		} else {
			m.exact[v.Pattern] = resolved{tMod, tMachine}
		}
	}
	return m
}

// vanillaMachines maps built-in vanilla recipe types to their canonical machine.
// Used as fallback when no valid_recipe_types entry matches.
var vanillaMachines = map[string]resolved{
	// Legacy pre-1.13 bare types (no minecraft: prefix)
	"crafting_shaped":    {modID: "minecraft", machineID: "crafting_table"},
	"crafting_shapeless": {modID: "minecraft", machineID: "crafting_table"},
	"smelting":           {modID: "minecraft", machineID: "furnace"},
	// Namespaced types
	"minecraft:smelting":                           {modID: "minecraft", machineID: "furnace"},
	"minecraft:blasting":                           {modID: "minecraft", machineID: "blast_furnace"},
	"minecraft:smoking":                            {modID: "minecraft", machineID: "smoker"},
	"minecraft:campfire_cooking":                   {modID: "minecraft", machineID: "campfire"},
	"minecraft:crafting_shaped":                    {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_shapeless":                 {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:stonecutting":                       {modID: "minecraft", machineID: "stonecutter"},
	"minecraft:smithing_transform":                 {modID: "minecraft", machineID: "smithing_table"},
	"minecraft:crafting_transmute":                 {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_imbue":                     {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_dye":                       {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_decorated_pot":             {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_firework_rocket":   {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_firework_star":     {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_firework_star_fade": {modID: "minecraft", machineID: "crafting_table"},
	"minecraft:crafting_special_shielddecoration":  {modID: "minecraft", machineID: "crafting_table"},
}

func (m matcher) resolve(recipeType string) (modID, machineID string, ok bool) {
	if r, found := m.exact[recipeType]; found {
		return resolveTarget(r, recipeType)
	}
	for _, entry := range m.regexes {
		if entry.re.MatchString(recipeType) {
			return resolveTarget(resolved{entry.targetMod, entry.targetMachine}, recipeType)
		}
	}
	// Fallback: built-in vanilla recipe types always resolve to their canonical machine.
	if r, found := vanillaMachines[recipeType]; found {
		return r.modID, r.machineID, true
	}
	return "", "", false
}

func resolveTarget(r resolved, recipeType string) (modID, machineID string, ok bool) {
	if r.modID != "" && r.machineID != "" {
		return r.modID, r.machineID, true
	}
	mod, machine, err := SplitTypeField(recipeType)
	if err != nil {
		return "", "", false
	}
	if r.modID != "" {
		mod = r.modID
	}
	if r.machineID != "" {
		machine = r.machineID
	}
	return mod, machine, true
}
