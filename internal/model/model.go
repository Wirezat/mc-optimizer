package model

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	IsAdmin      bool      `json:"is_admin"`
	PasswordHash string    `json:"-"` // never serialized
	CreatedAt    time.Time `json:"created_at"`
}

type Token struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TokenHash string    `json:"-"`    // never serialized
	Type      string    `json:"type"` // "session" | "refresh"
	ExpiresAt time.Time `json:"expires_at"`
}

type Save struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Factory struct {
	ID     uuid.UUID `json:"id"`
	SaveID uuid.UUID `json:"save_id"`
	Name   string    `json:"name"`
}

type ProductionLine struct {
	ID           uuid.UUID  `json:"id"`
	FactoryID    *uuid.UUID `json:"factory_id,omitempty"` // nil for Draft-PLs
	ParentPLID   *uuid.UUID `json:"parent_pl_id,omitempty"`
	Name         string     `json:"name"`
	TargetModID  string     `json:"target_mod_id"`
	TargetItemID string     `json:"target_item_id"`
	RateNum      int        `json:"rate_num"`
	RateDen      int        `json:"rate_den"`
	TimeUnit     string     `json:"time_unit"`     // "t" | "s" | "min" | "h"
	OptimizeMode string     `json:"optimize_mode"` // "TARGET" | "AUTO"
	Status       string     `json:"status"`        // "draft" | "active" | "archived"
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
	Status        string     `json:"status"` // "draft" | "planned" | "built" | "archived"
}

type PLIO struct {
	ID          uuid.UUID `json:"id"`
	PLID        uuid.UUID `json:"pl_id"`
	Direction   string    `json:"direction"` // "input" | "output"
	IOType      string    `json:"io_type"`   // "item" | "fluid"
	ModID       string    `json:"mod_id"`
	ItemFluidID string    `json:"item_fluid_id"`
	RateNum     int       `json:"rate_num"`
	RateDen     int       `json:"rate_den"`
	IsStopPoint bool      `json:"is_stop_point"`
}

type SolverDraft struct {
	ID        uuid.UUID `json:"id"`
	FactoryID uuid.UUID `json:"factory_id"`
	UserID    uuid.UUID `json:"user_id"`
	Result    []byte    `json:"result"` // JSONB — pass through as raw JSON in handlers
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// NormalizedRecipe is a parsed MI recipe ready for DB insertion.
type NormalizedRecipe struct {
	SourceFile  string
	RecipeType  string
	ModID       string
	MachineID   string
	EUPerTick   int64
	Duration    int
	ContentHash string

	ItemInputs   []NormalizedItemIO
	ItemOutputs  []NormalizedItemIO
	FluidInputs  []NormalizedFluidIO
	FluidOutputs []NormalizedFluidIO
}

type NormalizedItemIO struct {
	ItemModID *string
	ItemID    *string
	TagName   *string
	AmountNum int
	AmountDen int
	ProbNum   int
	ProbDen   int
}

type NormalizedFluidIO struct {
	FluidModID string
	FluidID    string
	AmountMB   int64
	ProbNum    int
	ProbDen    int
}

// ValidRecipeType maps a recipe type string (or regex pattern) to a canonical machine_type.
// If TargetModID/TargetMachineID are nil, the mod and machine IDs are taken directly from
// the recipe's type field.
type ValidRecipeType struct {
	ID              uuid.UUID `json:"id"`
	Pattern         string    `json:"pattern"`
	IsRegex         bool      `json:"is_regex"`
	TargetModID     *string   `json:"target_mod_id,omitempty"`
	TargetMachineID *string   `json:"target_machine_id,omitempty"`
}

type Mod struct {
	ModID      string `json:"mod_id"`
	Name       string `json:"name"`
	EnergyType string `json:"energy_type"`
}

type Item struct {
	ModID    string `json:"mod_id"`
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	MaxStack int16  `json:"max_stack"`
}

type Fluid struct {
	ModID   string `json:"mod_id"`
	FluidID string `json:"fluid_id"`
	Name    string `json:"name"`
}

type MachineType struct {
	ModID         string `json:"mod_id"`
	MachineID     string `json:"machine_id"`
	Name          string `json:"name"`
	BaseEUPerTick int64  `json:"base_eu_per_tick"`
	MaxEUPerTick  int64  `json:"max_eu_per_tick"`
	MaxSlots      int16  `json:"max_slots"`
	EnergyType    string `json:"energy_type"`
}

type Recipe struct {
	ID            string  `json:"id"`
	MachineModID  string  `json:"machine_mod_id"`
	MachineID     string  `json:"machine_id"`
	Name          *string `json:"name,omitempty"`
	DurationTicks int     `json:"duration_ticks"`
	EUPerTick     int64   `json:"eu_per_tick"`
	TotalEU       int64   `json:"total_eu"`
	Priority      int     `json:"priority"`

	ItemInputs   []RecipeItemInput   `json:"item_inputs"`
	ItemOutputs  []RecipeItemOutput  `json:"item_outputs"`
	FluidInputs  []RecipeFluidInput  `json:"fluid_inputs"`
	FluidOutputs []RecipeFluidOutput `json:"fluid_outputs"`
}

type RecipeItemInput struct {
	ID             string  `json:"id"`
	RecipeID       string  `json:"recipe_id"`
	ItemModID      *string `json:"item_mod_id,omitempty"` // nil when tag-based
	ItemID         *string `json:"item_id,omitempty"`
	ItemName       *string `json:"item_name,omitempty"`
	TagID          *string `json:"tag_id,omitempty"`   // nil when item-based
	TagName        *string `json:"tag_name,omitempty"` // resolved tag name, nil when item-based
	AmountNum      int     `json:"amount_num"`
	AmountDen      int     `json:"amount_den"`
	ProbabilityNum int     `json:"probability_num"`
	ProbabilityDen int     `json:"probability_den"`
}

type RecipeItemOutput struct {
	ID             string  `json:"id"`
	RecipeID       string  `json:"recipe_id"`
	ItemModID      string  `json:"item_mod_id"`
	ItemID         string  `json:"item_id"`
	ItemName       *string `json:"item_name,omitempty"`
	AmountNum      int     `json:"amount_num"`
	AmountDen      int     `json:"amount_den"`
	ProbabilityNum int     `json:"probability_num"`
	ProbabilityDen int     `json:"probability_den"`
}

type RecipeFluidInput struct {
	ID             string  `json:"id"`
	RecipeID       string  `json:"recipe_id"`
	FluidModID     string  `json:"fluid_mod_id"`
	FluidID        string  `json:"fluid_id"`
	FluidName      *string `json:"fluid_name,omitempty"`
	AmountMB       int64   `json:"amount_mb"`
	ProbabilityNum int     `json:"probability_num"`
	ProbabilityDen int     `json:"probability_den"`
}

type RecipeFluidOutput struct {
	ID             string  `json:"id"`
	RecipeID       string  `json:"recipe_id"`
	FluidModID     string  `json:"fluid_mod_id"`
	FluidID        string  `json:"fluid_id"`
	FluidName      *string `json:"fluid_name,omitempty"`
	AmountMB       int64   `json:"amount_mb"`
	ProbabilityNum int     `json:"probability_num"`
	ProbabilityDen int     `json:"probability_den"`
}

type CreateRecipeRequest struct {
	MachineID     string `json:"machine_id"`
	DurationTicks int    `json:"duration_ticks"`
	EUPerTick     int64  `json:"eu_per_tick"`
	Priority      int    `json:"priority"`

	ItemInputs   []CreateRecipeItemInput   `json:"item_inputs"`
	ItemOutputs  []CreateRecipeItemOutput  `json:"item_outputs"`
	FluidInputs  []CreateRecipeFluidInput  `json:"fluid_inputs"`
	FluidOutputs []CreateRecipeFluidOutput `json:"fluid_outputs"`
}

type CreateRecipeItemInput struct {
	ItemModID      *string `json:"item_mod_id"` // nil → tag-based
	ItemID         *string `json:"item_id"`
	TagID          *string `json:"tag_id"` // nil → item-based
	AmountNum      int     `json:"amount_num"`
	AmountDen      int     `json:"amount_den"`
	ProbabilityNum int     `json:"probability_num"`
	ProbabilityDen int     `json:"probability_den"`
}

type CreateRecipeItemOutput struct {
	ItemModID      string `json:"item_mod_id"`
	ItemID         string `json:"item_id"`
	AmountNum      int    `json:"amount_num"`
	AmountDen      int    `json:"amount_den"`
	ProbabilityNum int    `json:"probability_num"`
	ProbabilityDen int    `json:"probability_den"`
}

type CreateRecipeFluidInput struct {
	FluidModID     string `json:"fluid_mod_id"`
	FluidID        string `json:"fluid_id"`
	AmountMB       int64  `json:"amount_mb"`
	ProbabilityNum int    `json:"probability_num"`
	ProbabilityDen int    `json:"probability_den"`
}

type CreateRecipeFluidOutput struct {
	FluidModID     string `json:"fluid_mod_id"`
	FluidID        string `json:"fluid_id"`
	AmountMB       int64  `json:"amount_mb"`
	ProbabilityNum int    `json:"probability_num"`
	ProbabilityDen int    `json:"probability_den"`
}

// RecipeRow is the lean recipe representation used by the solver and db layer.
// It holds only what is needed for production rate calculations — no display names.
type RecipeRow struct {
	ID            string
	MachineMod    string
	MachineID     string
	DurationTicks int
	TotalEU       int64
	Priority      int
	ItemInputs    []RecipeRowItemInput
	ItemOutputs   []RecipeRowItemOutput
	FluidInputs   []RecipeRowFluidInput
	FluidOutputs  []RecipeRowFluidOutput
}

type RecipeRowItemInput struct {
	ItemModID      *string
	ItemID         *string
	TagID          *string
	AmountNum      int64
	AmountDen      int64
	ProbabilityNum int64
	ProbabilityDen int64
}

type RecipeRowItemOutput struct {
	ItemModID      string
	ItemID         string
	AmountNum      int64
	AmountDen      int64
	ProbabilityNum int64
	ProbabilityDen int64
}

type RecipeRowFluidInput struct {
	FluidModID     string
	FluidID        string
	AmountMB       int64
	ProbabilityNum int64
	ProbabilityDen int64
}

type RecipeRowFluidOutput struct {
	FluidModID     string
	FluidID        string
	AmountMB       int64
	ProbabilityNum int64
	ProbabilityDen int64
}

type UpgradeTier struct {
	ID             string
	ModID          string
	Name           string
	EUBonusPerSlot int64
	ItemModID      string
	ItemID         string
}
