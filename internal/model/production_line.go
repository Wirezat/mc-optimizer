package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ProductionLine struct {
	ID           uuid.UUID  `json:"id"`
	FactoryID    *uuid.UUID `json:"factory_id,omitempty"`
	ParentPLID   *uuid.UUID `json:"parent_pl_id,omitempty"`
	Name         string     `json:"name"`
	TargetModID    string     `json:"target_mod_id"`
	TargetItemID   string     `json:"target_item_id"`
	TargetIsFluid  bool       `json:"target_is_fluid"`
	TargetItemName string     `json:"target_item_name"`
	RateNum      int        `json:"rate_num"`
	RateDen      int        `json:"rate_den"`
	TimeUnit     string     `json:"time_unit"`
	OptimizeMode string     `json:"optimize_mode"`
	Status       string     `json:"status"`
	PLGroupID    *uuid.UUID `json:"pl_group_id,omitempty"`
	Position     string     `json:"position"`
	// SolveRequest is the original solver request, stored so the line can be re-solved
	// (e.g. at a higher target rate). Server-side only; not exposed in API responses.
	SolveRequest json.RawMessage `json:"-"`
}

type ProductionLineDetail struct {
	ProductionLine
	MachineGroups []*MachineGroup `json:"machine_groups"`
	IO            []*PLIO         `json:"io"`
}

type MachineGroup struct {
	ID            uuid.UUID  `json:"id"`
	PLID          uuid.UUID  `json:"pl_id"`
	MachineModID  string     `json:"machine_mod_id"`
	MachineID     string     `json:"machine_id"`
	RecipeID      uuid.UUID  `json:"recipe_id"`
	Count         int        `json:"count"`
	UpgradeTierID *uuid.UUID `json:"upgrade_tier_id,omitempty"`
	UpgradeCount  int        `json:"upgrade_count"`
	Status        string     `json:"status"`
	// ExactCount is the fractional machine count (recipeRate × effectiveTicks) before
	// rounding. Persisted so upgrade edits recompute Count losslessly from the true rate.
	ExactCountNum int64 `json:"exact_count_num"`
	ExactCountDen int64 `json:"exact_count_den"`
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
