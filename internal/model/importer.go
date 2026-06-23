package model

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
	Profession     string
	Tier           int
	CostModID      string
	CostItemID     string
	CostCount      int
	ResultModID    string
	ResultItemID   string
	ResultCount    int
	ResultModified bool
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
	SourceFile  string
	RecipeType  string
	ModID       string
	MachineID   string
	EUPerTick   int64
	Duration    int
	ContentHash string

	// Shape is a 9-element array for crafting_shaped recipes (row-major, 3×3).
	// Each element is "mod:item_id", "#mod:tag", or "" for an empty slot.
	Shape []string

	ItemInputs   []NormalizedIO
	ItemOutputs  []NormalizedIO
	FluidInputs  []NormalizedIO
	FluidOutputs []NormalizedIO
}

type NormalizedIO struct {
	ModID        *string
	ID           *string // item or fluid ID
	TagName      *string
	AmountNum    int   // items: rational numerator; fluids: 0
	AmountDen    int   // items: rational denominator; fluids: 0
	AmountMB     int64 // fluids: millibuckets; items: 0
	ProbNum      int
	ProbDen      int
	NonConsuming bool // true = item is a reusable tool (probability=0 in MI JSON)
}
