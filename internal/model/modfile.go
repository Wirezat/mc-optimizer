package model

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

type TagDef struct {
	Name    string   // e.g. "forge:ores/iron"
	Members []string // "mod_id:item_id"
}

type MachineTypeDef struct {
	ModID     string
	MachineID string
	Name      string // resolved from lang key at import time
	LangKey   string
	Slots     []MachineSlotDef
	// Implements lists base machine refs ("mod_id:machine_id" or plain "machine_id"
	// for same-mod) whose recipes this machine can also run — e.g. a steam-tier
	// machine implementing its electric base type. Written to machine_interfaces.
	Implements []string
	// Ecosystem names which plugin evaluates this machine; empty means the
	// machine's own mod_id.
	Ecosystem string
	// ModData holds every mod-specific field from mod.yml, keyed as written.
	// The host never interprets it, only passes it through to the plugin.
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
	ItemInputs    []ModIODef
	ItemOutputs   []ModIODef
	FluidInputs   []ModFluidIODef
	FluidOutputs  []ModFluidIODef
	// Shape is a 9-element row-major 3×3 array for crafting_shaped-style recipes
	// (each cell is a bare item id or tag name, "" for an empty cell). nil/empty
	// for non-shaped recipes.
	Shape []string
	// ModData holds every mod-specific field from the recipe entry, keyed as
	// written. The host never interprets it, only passes it through to the plugin.
	ModData map[string]any
}

type ModIODef struct {
	// Either ItemModID+ItemID or TagName is set.
	ItemModID *string
	ItemID    *string
	TagName   *string
	AmountNum int
	AmountDen int
	ProbNum   int
	ProbDen   int
}

type ModFluidIODef struct {
	FluidModID *string
	FluidID    *string
	TagName    *string
	AmountMB   int64
	ProbNum    int
	ProbDen    int
}
