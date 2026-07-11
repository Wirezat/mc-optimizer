package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ProductionLine struct {
	ID             uuid.UUID  `json:"id"`
	FactoryID      *uuid.UUID `json:"factory_id,omitempty"`
	ParentPLID     *uuid.UUID `json:"parent_pl_id,omitempty"`
	Name           string     `json:"name"`
	TargetModID    string     `json:"target_mod_id"`
	TargetItemID   string     `json:"target_item_id"`
	TargetIsFluid  bool       `json:"target_is_fluid"`
	TargetItemName string     `json:"target_item_name"`
	RateNum        int        `json:"rate_num"`
	RateDen        int        `json:"rate_den"`
	TimeUnit       string     `json:"time_unit"`
	OptimizeMode   string     `json:"optimize_mode"`
	Status         string     `json:"status"`
	PLGroupID      *uuid.UUID `json:"pl_group_id,omitempty"`
	Position       string     `json:"position"`
	// SolveRequest is the original solver request, stored so the line can be re-solved
	// (e.g. at a higher target rate). Server-side only; not exposed in API responses.
	SolveRequest json.RawMessage `json:"-"`
	// TargetEUPerTick/CurrentEUPerTick are computed (not persisted) rollups across this
	// line's machine groups — sum of count×EUPerTick vs. built_count×CurrentEUPerTick.
	// Populated by Get/ListProductionLinesHandler; omitted elsewhere.
	TargetEUPerTick  int64 `json:"target_eu_per_tick,omitempty"`
	CurrentEUPerTick int64 `json:"current_eu_per_tick,omitempty"`
	// CurrentRate is the estimated real output rate (same unit as RateNum/RateDen) given
	// the line's current build state — the target rate scaled by the worst-bottlenecked
	// machine group's built/upgraded throughput fraction, since a chain's output is
	// limited by its slowest link. Computed (not persisted); populated only by
	// GetProductionLineHandler (see db.EstimateCurrentRateFraction).
	CurrentRate float64 `json:"current_rate"`
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
	// Current (in-game) build state, independent of Count/UpgradeTierID/UpgradeCount above —
	// a player often builds fewer machines than planned, or without the full upgrade
	// loadout yet, and needs to track that partial state separately from the target.
	// Current upgrades always track toward UpgradeTierID (the group's own target tier);
	// only the count is independently tracked. Like UpgradeCount, this is per machine.
	BuiltCount          int `json:"built_count"`
	CurrentUpgradeCount int `json:"current_upgrade_count"`
	// EUPerTick/CurrentEUPerTick are computed (not persisted): the per-machine EU/t draw
	// at the target vs. current upgrade loadout, using the same formula as the solver
	// (solver.EffectiveEUPerTick). Populated only by GetProductionLineHandler; zero on
	// list/insert paths that don't enrich groups. Multiply by Count/BuiltCount for the
	// group's total draw.
	EUPerTick        int64 `json:"eu_per_tick,omitempty"`
	CurrentEUPerTick int64 `json:"current_eu_per_tick,omitempty"`
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
