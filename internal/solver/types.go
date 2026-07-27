package solver

import "fmt"

const (
	TicksPerSecond = 20
	TicksPerMinute = 1200
	TicksPerHour   = 72000
)

type SolveMode string

const (
	SolveModeTarget SolveMode = "TARGET"
	SolveModeAuto   SolveMode = "AUTO"
)

// UpgradeMode controls how the solver applies machine upgrades.
type UpgradeMode string

const (
	// UpgradeModeOff applies no upgrades; machines run at their base capacity.
	UpgradeModeOff UpgradeMode = "off"
	// UpgradeModeFixed applies a user-chosen tier and count to all upgradable groups.
	UpgradeModeFixed UpgradeMode = "fixed"
	// UpgradeModeAuto picks tier and count per group to minimise machine count.
	UpgradeModeAuto UpgradeMode = "auto"
)

// defaultBaseMaxEU is the per-tick recipe cap for single-block electric (MI tier LV)
// machines when machine_types.max_eu_per_tick is not set. Multiblock machines (128) can
// be modelled by storing max_eu_per_tick explicitly in the DB.
const defaultBaseMaxEU int64 = 32

// maxUpgradeSlots caps the upgrade count when a machine has no max_slots recorded
// (MI upgrade stacks are limited to a single stack of 64).
const maxUpgradeSlots = 64

type DraftStatus string

const (
	StatusDraft DraftStatus = "draft"
)

// ConvertToPerTick converts a rate from the given time unit to per-tick.
func ConvertToPerTick(rate Rational, unit string) (Rational, error) {
	switch unit {
	case "t":
		return rate, nil
	case "s":
		return rate.Div(RationalFromInt(TicksPerSecond)), nil
	case "min":
		return rate.Div(RationalFromInt(TicksPerMinute)), nil
	case "h":
		return rate.Div(RationalFromInt(TicksPerHour)), nil
	default:
		return Rational{}, fmt.Errorf("solver: unknown time unit: %s", unit)
	}
}

// ConvertFromPerTick converts a per-tick rate to the given time unit.
func ConvertFromPerTick(rate Rational, unit string) Rational {
	switch unit {
	case "s":
		return rate.Mul(RationalFromInt(TicksPerSecond))
	case "min":
		return rate.Mul(RationalFromInt(TicksPerMinute))
	case "h":
		return rate.Mul(RationalFromInt(TicksPerHour))
	default: // "t" or unknown
		return rate
	}
}

type ItemRef struct {
	ModID   string
	ItemID  string
	TagRef  string // if set, this is a tag requirement rather than a specific item
	IsFluid bool   // true for fluid inputs/outputs (ItemID holds the fluid ID)
}

// Key returns the canonical string key for use in maps.
func (i *ItemRef) Key() string {
	if i.TagRef != "" {
		return "#" + i.TagRef
	}
	if i.IsFluid {
		return "fluid:" + i.ModID + ":" + i.ItemID
	}
	return i.ModID + ":" + i.ItemID
}

type SolveRequest struct {
	TargetItem           ItemRef
	TargetRate           Rational
	TimeUnit             string
	Mode                 SolveMode
	StopPoints           map[string]bool
	RecipeOverrides      map[string]string
	TagOverrides         map[string]string // tagName → "mod_id:item_id"
	FactoryState         FactoryState
	AllowPartialMachines []string

	// Upgrade options (MI machine upgrades). UpgradeMode defaults to off when empty.
	UpgradeMode  UpgradeMode
	UpgradeTier  string   // tier ID, used in fixed mode
	UpgradeCount int      // slot count, used in fixed mode
	AllowedTiers []string // tier IDs the auto mode may use; empty = all
}

// TagResolution describes how a tag was resolved during solving.
type TagResolution struct {
	Chosen  ItemRef
	Options []ItemRef
}

type FactoryState struct {
	ExistingOutputs map[string]Rational
}

// Warning is a structured, translatable solver warning. Code maps to the i18n key
// solve.warning.<code>; Params are interpolated into the translated text by the client.
type Warning struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

type SolveResult struct {
	MachineGroups  []MachineGroupDraft
	IOProfile      IOProfile
	ActualRate     Rational
	HadCycles      bool
	ModeUsed       SolveMode
	Warnings       []Warning
	TagResolutions map[string]TagResolution // tagKey → resolution
}

type MachineRef struct {
	ModID     string
	MachineID string
}

type MachineGroupDraft struct {
	MachineMod   string
	MachineID    string
	RecipeID     string
	RecipeOutput ItemRef // item this recipe produces
	Count        int64
	ExactCount   Rational // fractional machine count before ceiling
	Utilization  Rational
	UpgradeTier  string
	UpgradeCount int
	Status       DraftStatus
	EUPerTick    int64 // effective EU/t drawn by ONE machine in this group; 0 for non-eu machines
}

type IOProfile struct {
	Inputs  []IOEntry
	Outputs []IOEntry
}

type IOEntry struct {
	Item              ItemRef
	Rate              Rational
	TimeUnit          string
	IsStopPoint       bool
	IsFactoryProvided bool
}

// ErrCycleBreakNeeded is returned by Solve when a cycle exists that cannot be
// resolved automatically. The caller must choose a stop point from CycleNodes
// and re-submit the request with that item added to StopPoints.
type ErrCycleBreakNeeded struct {
	CycleNodes []string // ItemRef.Key() values of stuck nodes
}

func (e *ErrCycleBreakNeeded) Error() string {
	return fmt.Sprintf("cycle break needed: %v", e.CycleNodes)
}
