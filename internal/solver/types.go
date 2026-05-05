package solver

// Tick constants.
// All internal rates are stored as per-tick values.
// 1 minute = 1200 ticks (20 TPS × 60s)
const (
	TicksPerSecond = 20
	TicksPerMinute = 1200
	TicksPerHour   = 72000
)

// SolveMode controls how the solver determines machine counts.
type SolveMode string

const (
	SolveModeTarget SolveMode = "TARGET" // hit exactly the requested rate
	SolveModeAuto   SolveMode = "AUTO"   // round up to whole machines
)

// DraftStatus is the lifecycle state of a MachineGroupDraft.
type DraftStatus string

const (
	StatusDraft DraftStatus = "draft"
)

// ConvertToPerTick converts a rate from the given time unit to per-tick.
// Valid units:
//   - "t"   — already per-tick; returned unchanged
//   - "s"   — per second  (÷ 20)
//   - "min" — per minute  (÷ 1200)
//   - "h"   — per hour    (÷ 72000)
//
// Panics on unknown units.
func ConvertToPerTick(rate Rational, unit string) Rational {
	switch unit {
	case "t":
		return rate
	case "s":
		return rate.Div(RationalFromInt(TicksPerSecond))
	case "min":
		return rate.Div(RationalFromInt(TicksPerMinute))
	case "h":
		return rate.Div(RationalFromInt(TicksPerHour))
	default:
		panic("solver: unknown time unit: " + unit)
	}
}

// ItemRef uniquely identifies an item within a mod.
type ItemRef struct {
	ModID  string
	ItemID string
}

// Key returns the canonical string key for use in maps.
func (i ItemRef) Key() string {
	return i.ModID + ":" + i.ItemID
}

// SolveRequest is the input to the solver.
type SolveRequest struct {
	TargetItem      ItemRef
	TargetRate      Rational
	TimeUnit        string // unit of TargetRate before conversion ("t", "s", "min", "h")
	Mode            SolveMode
	StopPoints      map[string]bool   // ItemRef.Key() → true; solver treats these items as raw inputs
	RecipeOverrides map[string]string // ItemRef.Key() → recipe UUID; overrides default recipe selection
	FactoryState    FactoryState
}

// FactoryState describes what the factory already produces,
// allowing the solver to account for existing output when calculating additional need.
type FactoryState struct {
	ExistingOutputs map[string]Rational // ItemRef.Key() → rate per tick
}

// SolveResult is the output of the solver.
type SolveResult struct {
	MachineGroups []MachineGroupDraft
	IOProfile     IOProfile
	ActualRate    Rational // actual achieved rate per tick (may exceed target in AUTO mode)
	HadCycles     bool     // true if the recipe graph contained dependency cycles
	ModeUsed      SolveMode
	Warnings      []string
}

// MachineGroupDraft describes a group of identical machines running the same recipe.
// Status is always StatusDraft until persisted to the database.
type MachineGroupDraft struct {
	MachineMod   string
	MachineID    string
	RecipeID     string
	Count        int64
	Utilization  Rational // fraction of full capacity used (0–1 in TARGET mode, always 1 in AUTO)
	UpgradeTier  string
	UpgradeCount int
	Status       DraftStatus
}

// IOProfile summarises the net item flow of a solved production line.
type IOProfile struct {
	Inputs  []IOEntry
	Outputs []IOEntry
}

// IOEntry is a single item flow in an IOProfile.
// Rate is stored internally as per-tick; TimeUnit records the display unit
// for rendering in the frontend (e.g. "min" → show as items/min).
type IOEntry struct {
	Item     ItemRef
	Rate     Rational
	TimeUnit string // display unit — does not affect Rate value
}
