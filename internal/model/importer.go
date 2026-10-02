package model

import "github.com/Wirezat/production-optimizer/internal/resource"

// BlockDrop describes what a block yields when broken.
type BlockDrop struct {
	BlockModID  string
	BlockItemID string
	DropModID   string
	DropItemID  string
	MinCount    int
	MaxCount    int
	Condition   string // "normal" | "silk_touch" | "fortune"
}

// VillagerTrade is one trade offer from a villager profession.
type VillagerTrade struct {
	SourceModID    string
	TradeKey       string
	Profession     string
	Tier           int
	CostModID      string
	CostItemID     string
	CostCount      int
	Cost2ModID     string
	Cost2ItemID    string
	Cost2Count     int
	ResultModID    string
	ResultItemID   string
	ResultCount    int
	ResultModified bool
	CostVariable   bool
	MaxUses        *int
	XP             *int
}

// ModMetadata holds optional enrichment data fetched from Modrinth.
type ModMetadata struct {
	ModID        string `json:"mod_id"`
	Description  string `json:"description"`
	Author       string `json:"author"`
	License      string `json:"license"`
	URLSource    string `json:"url_source"`
	URLModrinth  string `json:"url_modrinth"`
	URLWiki      string `json:"url_wiki"`
	URLIssues    string `json:"url_issues"`
	URLDiscord   string `json:"url_discord"`
	ModrinthSlug string `json:"modrinth_slug"`
}

type NormalizedRecipe struct {
	SourceFile string
	RecipeType string
	ModID      string
	// machine's mod) for addon-added recipes.
	SourceModID string
	MachineID   string
	Duration    int
	ContentHash string

	// Shape is a 9-element array for crafting_shaped recipes (row-major, 3×3).
	Shape []string

	// ModData holds every mod-specific field from the recipe entry, keyed as written.
	ModData map[string]any

	Inputs  []resource.IO
	Outputs []resource.IO
}
