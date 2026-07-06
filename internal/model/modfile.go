package model

// ModDef holds all data parsed from a mod YAML file.
type ModDef struct {
	ModID        string
	Name         string
	EnergyType   string
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
	UpgradeTiers   []UpgradeTierDef
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

type UpgradeTierDef struct {
	Name           string
	EUBonusPerSlot int64
	ItemRef        string // "mod_id:item_id" — the item's own max_stack is the upgrade count cap
}

type MachineTypeDef struct {
	ModID             string
	MachineID         string
	Name              string // resolved from lang key at import time
	LangKey           string
	Ecosystem         string // "vanilla" | "modern_industrialization" | "mekanism" | ...
	BaseEnergyPerTick *int64
	MaxEnergyPerTick  *int64
	MaxSlots          *int16
	EnergyType        *string // overrides mod-level energy_type if set
	Upgradable        bool
	Slots             []MachineSlotDef
	// Implements lists base machine refs ("mod_id:machine_id" or plain "machine_id"
	// for same-mod) whose recipes this machine can also run — e.g. a steam-tier
	// machine implementing its electric base type. Written to machine_interfaces.
	Implements []string
	// FixedRecipeEUCap, if set, is an additional ceiling on recipe eu/t that upgrades
	// never raise — e.g. MI's Electric Blast Furnace coil tiers (cupronickel=32 vs.
	// kanthal=128), independent of the normal MaxEnergyPerTick+upgrade-bonus cap.
	FixedRecipeEUCap *int64
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
	EnergyPerTick *int64
	ItemInputs    []ModIODef
	ItemOutputs   []ModIODef
	FluidInputs   []ModFluidIODef
	FluidOutputs  []ModFluidIODef
	// Shape is a 9-element row-major 3×3 array for crafting_shaped-style recipes
	// (each cell is a bare item id or tag name, "" for an empty cell). nil/empty
	// for non-shaped recipes.
	Shape []string
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
