package model

type Mod struct {
	ModID        string  `json:"mod_id"`
	Name         string  `json:"name"`
	EnergyType   string  `json:"energy_type"`
	Description  *string `json:"description,omitempty"`
	Author       *string `json:"author,omitempty"`
	License      *string `json:"license,omitempty"`
	URLSource    *string `json:"url_source,omitempty"`
	URLModrinth  *string `json:"url_modrinth,omitempty"`
	URLWiki      *string `json:"url_wiki,omitempty"`
	URLIssues    *string `json:"url_issues,omitempty"`
	URLDiscord   *string `json:"url_discord,omitempty"`
	ModrinthSlug *string `json:"modrinth_slug,omitempty"`
}

// ModUpdate carries the fields an admin may overwrite on a mod.
// nil pointer = keep existing value; empty string = clear the field.
type ModUpdate struct {
	Name         *string
	EnergyType   *string
	Description  *string
	Author       *string
	License      *string
	URLSource    *string
	URLModrinth  *string
	URLWiki      *string
	URLIssues    *string
	URLDiscord   *string
	ModrinthSlug *string
}

type Item struct {
	ModID    string `json:"mod_id"`
	ItemID   string `json:"item_id"`
	Name     string `json:"name"` // populated from translations (en_us); empty until JAR import
	MaxStack int16  `json:"max_stack"`
}

type Fluid struct {
	ModID   string `json:"mod_id"`
	FluidID string `json:"fluid_id"`
	Name    string `json:"name"` // populated from translations (en_us); empty until JAR import
}

type MachineType struct {
	ModID         string `json:"mod_id"`
	MachineID     string `json:"machine_id"`
	Name          string `json:"name"`
	BaseEUPerTick int64  `json:"base_eu_per_tick"`
	MaxEUPerTick  int64  `json:"max_eu_per_tick"`
	MaxSlots      int16  `json:"max_slots"`
	EnergyType    string `json:"energy_type"`
	Upgradable    bool   `json:"upgradable"`
	RecipeCount   int    `json:"recipe_count"`
}

type UpgradeTier struct {
	ID             string `json:"id"`
	ModID          string `json:"mod_id"`
	Name           string `json:"name"`
	EUBonusPerSlot int64  `json:"eu_bonus_per_slot"`
	ItemModID      string `json:"item_mod_id"`
	ItemID         string `json:"item_id"`
}

type ValidRecipeType struct {
	ID              string  `json:"id"`
	Pattern         string  `json:"pattern"`
	IsRegex         bool    `json:"is_regex"`
	TargetModID     *string `json:"target_mod_id,omitempty"`
	TargetMachineID *string `json:"target_machine_id,omitempty"`
}

type MachineSlot struct {
	SlotIndex int16   `json:"slot_index"`
	SlotType  string  `json:"slot_type"`
	SlotX     *int16  `json:"slot_x,omitempty"`
	SlotY     *int16  `json:"slot_y,omitempty"`
	Label     *string `json:"label,omitempty"`
}

type VillagerTradeView struct {
	ID             string  `json:"id"`
	Profession     string  `json:"profession"`
	Tier           int     `json:"tier"`
	CostModID      string  `json:"cost_mod_id"`
	CostItemID     string  `json:"cost_item_id"`
	CostName       string  `json:"cost_name"`
	CostCount      int     `json:"cost_count"`
	ResultModID    string  `json:"result_mod_id"`
	ResultItemID   string  `json:"result_item_id"`
	ResultName     string  `json:"result_name"`
	ResultCount    int     `json:"result_count"`
	ResultModified bool    `json:"result_modified"`
	MaxUses        *int    `json:"max_uses,omitempty"`
	XP             *int    `json:"xp,omitempty"`
}
