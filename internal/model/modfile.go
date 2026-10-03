package model

import "github.com/Wirezat/production-optimizer/internal/resource"

// ModDef holds all data parsed from a mod YAML file.
type ModDef struct {
	ModID        string
	Name         string
	Description  string
	Author       string
	License      string
	ModrinthSlug string
	URLSource    string
	URLModrinth  string
	URLWiki      string
	URLIssues    string
	URLDiscord   string

	Translations   map[string]map[string]string // lang → key → value
	Items          []ItemDef
	Fluids         []FluidDef
	Energies       []EnergyDef
	Tags           []TagDef
	Machines       []MachineTypeDef
	Recipes        []ModRecipeDef
	BlockDrops     []BlockDrop
	VillagerTrades []VillagerTrade
}

type ItemDef struct {
	ModID    string
	ItemID   string
	MaxStack int
	LangKey  string
}

type FluidDef struct {
	ModID   string
	FluidID string
	LangKey string
}

type EnergyDef struct {
	ModID     string
	EnergyID  string
	Symbol    string
	LangKey   string
	FePerUnit resource.Rational
}

const (
	TagKindItem  = "item"
	TagKindFluid = "fluid"
)

type TagDef struct {
	Name    string   // e.g. "forge:ores/iron"
	Kind    string   // TagKindItem or TagKindFluid
	Members []string // "mod_id:item_id" or "mod_id:fluid_id"
}

type MachineTypeDef struct {
	ModID      string
	MachineID  string
	Name       string // resolved from lang key at import time
	LangKey    string
	Slots      []MachineSlotDef
	Implements []string
	// Ecosystem names the plugin's mod_id; empty means the machine's own.
	Ecosystem string
	// ModData holds every mod-specific field from mod.yml, keyed as written.
	ModData map[string]any
}

type MachineSlotDef struct {
	ModID     string
	MachineID string
	Index     int
	SlotType  string // "item_input" | "item_output" | "fluid_input" | "fluid_output"
	X         *int16
	Y         *int16
	Label     *string
}

// ModRecipeDef is a recipe as parsed from a mod YAML file (already normalized).
type ModRecipeDef struct {
	MachineModID  string
	MachineID     string
	DurationTicks int
	Inputs        []resource.IO
	Outputs       []resource.IO
	// Shape is a row-major 3×3 grid of item ids or tag names, "" for an empty cell.
	Shape []string
	// ModData holds every mod-specific field from the recipe entry, keyed as written.
	ModData map[string]any
}
