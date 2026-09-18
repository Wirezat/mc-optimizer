package solver

import (
	"encoding/json"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

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
	AllowPartialMachines []string // groups that may stand idle, by RateKey
	// ModConfigs holds each mod's opaque plugin config, keyed by the mod that owns the plugin
	// (a machine's ecosystem, or its own mod id).
	ModConfigs map[string]json.RawMessage
	// Factor is the manual override on the solver's own answer: the line is this multiple of
	// it. The zero value means x1.
	Factor Rational
	// VariantPins fixes the operating variant of individual machine groups, keyed by
	// RecipeOptionKey.
	VariantPins map[string]string
}

// TagResolution describes how a tag was resolved during solving.
type TagResolution struct {
	Chosen  ItemRef
	Options []ItemRef
}

type FactoryState struct {
	ExistingOutputs map[string]Rational
}

// Warning is a structured, translatable solver warning.
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
	Status       DraftStatus
	// VariantID, Label and Costs describe the operating configuration chosen for this group;
	// Variant carries it whole, including any output overrides.
	VariantID string
	Label     string
	// PluginMod is the mod whose plugin evaluated this group.
	PluginMod string
	Costs     []plugins.Cost
	Variant   plugins.Variant
	// VariantOptions lists every runnable variant of this group so a client can offer the
	// alternatives the automatic pick did not take.
	VariantOptions []VariantOption
	// RateKey names the node this group was computed for, and is the only stable handle on it:
	// the ladder may swap the group's machine or recipe.
	RateKey string
	cells   []cell // the matrix the pick was made from, for the AUTO repick
}

func (g *MachineGroupDraft) applyCell(c cell) {
	g.MachineMod, g.MachineID = c.machine.ModID, c.machine.MachineID
	g.RecipeID = c.recipe.ID
	g.PluginMod = PluginMod(c.machine)
	g.Variant, g.VariantID, g.Label, g.Costs = c.variant, c.variant.ID, c.variant.Label, c.variant.Costs
	g.ExactCount, g.Count, g.Utilization = c.exact, c.count, c.utilization
	g.VariantOptions = variantOptionsFor(g.cells, c.machine)
}

// VariantOption is one selectable operating variant of a machine group, as offered to a
// client.
type VariantOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
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

// ErrCycleBreakNeeded is returned by Solve when a cycle exists that cannot be resolved
// automatically.
type ErrCycleBreakNeeded struct {
	CycleNodes []string // ItemRef.Key() values of stuck nodes
}

func (e *ErrCycleBreakNeeded) Error() string {
	return fmt.Sprintf("cycle break needed: %v", e.CycleNodes)
}
