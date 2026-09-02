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
	// Costs is the line's total operating cost per tick, summed across its
	// machine groups' chosen variants. Computed on demand from the variant
	// cache, never stored. ListProductionLinesHandler always sets it to a
	// list (possibly empty); handlers that don't populate it leave it null.
	Costs []plugins.Cost `json:"costs"`
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
	// ModConfig overrides the save-wide plugin config for this group; an empty
	// object means the save-wide config applies. Opaque to the host.
	ModConfig json.RawMessage `json:"mod_config,omitempty"`
	// VariantID is the operating variant this group targets, CurrentVariantID
	// the one actually built in-game.
	VariantID        string `json:"variant_id"`
	CurrentVariantID string `json:"current_variant_id"`
	ExactCountNum    int64  `json:"exact_count_num"`
	ExactCountDen    int64  `json:"exact_count_den"`
	BuiltCount       int    `json:"built_count"`
	// Costs is the chosen variant's operating cost per tick. Computed on
	// demand from the plugin, never stored.
	Costs []plugins.Cost `json:"costs,omitempty"`
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
