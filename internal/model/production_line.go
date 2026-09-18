package model

import (
	"encoding/json"
	"time"

	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/google/uuid"
)

type ProductionLine struct {
	ID             uuid.UUID       `json:"id"`
	FactoryID      *uuid.UUID      `json:"factory_id,omitempty"`
	ParentPLID     *uuid.UUID      `json:"parent_pl_id,omitempty"`
	TargetModID    string          `json:"target_mod_id"`
	TargetItemID   string          `json:"target_item_id"`
	TargetIsFluid  bool            `json:"target_is_fluid"`
	TargetItemName string          `json:"target_item_name"`
	RateNum        int             `json:"rate_num"`
	RateDen        int             `json:"rate_den"`
	TimeUnit       string          `json:"time_unit"`
	OptimizeMode   string          `json:"optimize_mode"`
	Status         string          `json:"status"`
	PLGroupID      *uuid.UUID      `json:"pl_group_id,omitempty"`
	Position       string          `json:"position"`
	ModMissing     bool            `json:"mod_missing"`
	SolveRequest   json.RawMessage `json:"-"`
	CurrentRate    float64         `json:"current_rate"`
	// Costs is the line's total operating cost per tick, summed across its machine groups'
	// chosen variants.
	Costs []plugins.Cost `json:"costs"`
	// Machines is what the line runs on, one entry per machine kind with the counts of every
	// group using it added up.
	Machines []MachineUse `json:"machines"`
}

// MachineUse is one machine kind a production line runs, and how many of it.
type MachineUse struct {
	MachineModID string `json:"machine_mod_id"`
	MachineID    string `json:"machine_id"`
	Count        int    `json:"count"`
}

type ProductionLineDetail struct {
	ProductionLine
	MachineGroups []*MachineGroup `json:"machine_groups"`
	IO            []*PLIO         `json:"io"`
}

type MachineGroup struct {
	ID           uuid.UUID `json:"id"`
	PLID         uuid.UUID `json:"pl_id"`
	MachineModID string    `json:"machine_mod_id"`
	MachineID    string    `json:"machine_id"`
	RecipeID     uuid.UUID `json:"recipe_id"`
	Count        int       `json:"count"`
	Status       string    `json:"status"`
	// ModConfig overrides the save-wide plugin config for this group; an empty object means
	// the save-wide config applies.
	ModConfig json.RawMessage `json:"mod_config,omitempty"`
	// VariantID is the operating variant this group targets, CurrentVariantID the one actually
	// built in-game.
	VariantID        string `json:"variant_id"`
	CurrentVariantID string `json:"current_variant_id"`
	ExactCountNum    int64  `json:"exact_count_num"`
	ExactCountDen    int64  `json:"exact_count_den"`
	BuiltCount       int    `json:"built_count"`
	// Costs is the chosen variant's operating cost per tick.
	Costs []plugins.Cost `json:"costs,omitempty"`
	// Variant and CurrentVariant describe the target and the built operating variant;
	// VariantOptions lists every runnable one.
	Variant        *VariantView  `json:"variant,omitempty"`
	CurrentVariant *VariantView  `json:"current_variant,omitempty"`
	VariantOptions []VariantView `json:"variant_options,omitempty"`
}

// VariantView is an operating variant as a client renders it: the plugin's label and the
// items one machine needs installed.
type VariantView struct {
	ID    string        `json:"id"`
	Label string        `json:"label"`
	Items []VariantItem `json:"items"`
}

// VariantItem is one installed item kind of a variant, count per machine.
type VariantItem struct {
	Ref   string `json:"ref"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type PLIO struct {
	ID          uuid.UUID `json:"id"`
	PLID        uuid.UUID `json:"pl_id"`
	Direction   string    `json:"direction"`
	IOType      string    `json:"io_type"`
	ModID       string    `json:"mod_id"`
	ItemFluidID string    `json:"item_fluid_id"`
	Name        string    `json:"name"`
	RateNum     int       `json:"rate_num"`
	RateDen     int       `json:"rate_den"`
	IsStopPoint bool      `json:"is_stop_point"`
}

type SolverDraft struct {
	ID        uuid.UUID `json:"id"`
	FactoryID uuid.UUID `json:"factory_id"`
	UserID    uuid.UUID `json:"user_id"`
	Result    []byte    `json:"result"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
