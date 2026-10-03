package model

import (
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/resource"
)

type Mod struct {
	ModID        string  `json:"mod_id"`
	Name         string  `json:"name"`
	Description  *string `json:"description,omitempty"`
	Author       *string `json:"author,omitempty"`
	License      *string `json:"license,omitempty"`
	URLSource    *string `json:"url_source,omitempty"`
	URLModrinth  *string `json:"url_modrinth,omitempty"`
	URLWiki      *string `json:"url_wiki,omitempty"`
	URLIssues    *string `json:"url_issues,omitempty"`
	URLDiscord   *string `json:"url_discord,omitempty"`
	ModrinthSlug *string `json:"modrinth_slug,omitempty"`
	RecipeCount  int     `json:"recipe_count"`
	ItemCount    int     `json:"item_count"`
	// Ecosystems is every distinct ecosystem this mod's machines declare.
	Ecosystems []string       `json:"ecosystems"`
	Plugin     *ModPluginInfo `json:"plugin,omitempty"`
}

// ModPluginInfo labels an installed plugin's wizard button, if it has one.
type ModPluginInfo struct {
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
	HasWizard   bool   `json:"has_wizard"`
}

type ModUpdate struct {
	Name         *string
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
	ModID      string            `json:"mod_id"`
	ItemID     string            `json:"item_id"`
	Name       string            `json:"name"` // populated from translations (en_us); empty until JAR import
	MaxStack   int16             `json:"max_stack"`
	TextureURL *string           `json:"texture_url,omitempty"`
	Animation  *TextureAnimation `json:"animation,omitempty"`
}

// TextureAnimation describes a sprite sheet whose frames are stacked downward in the file.
type TextureAnimation struct {
	// Cells is how many frames are stacked in the file.
	Cells int `json:"cells"`
	// Frames is how many cells the client should play.
	Frames  int `json:"frames"`
	FrameMS int `json:"frame_ms"`
	// PingPong plays the frames up and back down again.
	PingPong bool `json:"ping_pong,omitempty"`
}

// TagMember is one item or fluid a tag stands for.
type TagMember struct {
	Kind    string `json:"kind"`
	TagName string `json:"tag_name"`
	ModID   string `json:"mod_id"`
	ID      string `json:"id"`
}

type Fluid struct {
	ModID      string            `json:"mod_id"`
	FluidID    string            `json:"fluid_id"`
	Name       string            `json:"name"` // populated from translations (en_us); empty until JAR import
	TextureURL *string           `json:"texture_url,omitempty"`
	Animation  *TextureAnimation `json:"animation,omitempty"`
}

// Energy is an energy form; Name falls back to Symbol without a translation.
type Energy struct {
	ModID     string            `json:"mod_id"`
	EnergyID  string            `json:"energy_id"`
	Symbol    string            `json:"symbol"`
	Name      string            `json:"name"`
	FePerUnit resource.Rational `json:"fe_per_unit"`
}

type MachineType struct {
	ModID       string  `json:"mod_id"`
	MachineID   string  `json:"machine_id"`
	Name        string  `json:"name"`
	RecipeCount int     `json:"recipe_count"`
	TextureURL  *string `json:"texture_url,omitempty"`
	// Variants holds every machine folded into this row, alphabetical; nil for a single machine.
	Variants []MachineVariant `json:"variants,omitempty"`
	// Costs is the base variant's operating cost; nil when none is cached yet.
	Costs []plugins.Cost `json:"costs,omitempty"`
}

// MachineVariant is one machine folded into a grouped MachineType row.
type MachineVariant struct {
	ModID      string  `json:"mod_id"`
	MachineID  string  `json:"machine_id"`
	Name       string  `json:"name"`
	TextureURL *string `json:"texture_url,omitempty"`
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
	SourceModID    string  `json:"source_mod_id"`
	SourceName     string  `json:"source_name"`
	Profession     string  `json:"profession"`
	Tier           int     `json:"tier"`
	CostModID      string  `json:"cost_mod_id"`
	CostItemID     string  `json:"cost_item_id"`
	CostName       string  `json:"cost_name"`
	CostCount      int     `json:"cost_count"`
	Cost2ModID     *string `json:"cost2_mod_id,omitempty"`
	Cost2ItemID    *string `json:"cost2_item_id,omitempty"`
	Cost2Name      *string `json:"cost2_name,omitempty"`
	Cost2Count     *int    `json:"cost2_count,omitempty"`
	ResultModID    string  `json:"result_mod_id"`
	ResultItemID   string  `json:"result_item_id"`
	ResultName     string  `json:"result_name"`
	ResultCount    int     `json:"result_count"`
	ResultModified bool    `json:"result_modified"`
	// CostVariable marks an offer whose price the data does not fix; the counts are a floor.
	CostVariable bool `json:"cost_variable"`
	MaxUses      *int `json:"max_uses,omitempty"`
	XP           *int `json:"xp,omitempty"`
}

// ModBlocker is one reason a mod cannot be deleted; Sample holds up to three names out of Count.
type ModBlocker struct {
	Kind   string   `json:"kind"`
	Count  int      `json:"count"`
	Sample []string `json:"sample"`
}
