package solver

import (
	"encoding/json"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/resource"
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
func ConvertToPerTick(rate resource.Rational, unit string) (resource.Rational, error) {
	switch unit {
	case "t":
		return rate, nil
	case "s":
		return rate.Div(resource.RationalFromInt(TicksPerSecond)), nil
	case "min":
		return rate.Div(resource.RationalFromInt(TicksPerMinute)), nil
	case "h":
		return rate.Div(resource.RationalFromInt(TicksPerHour)), nil
	default:
		return resource.Rational{}, fmt.Errorf("solver: unknown time unit: %s", unit)
	}
}

// ConvertFromPerTick converts a per-tick rate to the given time unit.
func ConvertFromPerTick(rate resource.Rational, unit string) resource.Rational {
	switch unit {
	case "s":
		return rate.Mul(resource.RationalFromInt(TicksPerSecond))
	case "min":
		return rate.Mul(resource.RationalFromInt(TicksPerMinute))
	case "h":
		return rate.Mul(resource.RationalFromInt(TicksPerHour))
	default: // "t" or unknown
		return rate
	}
}

type SolveRequest struct {
	TargetItem           resource.Ref               `json:"target"`
	TargetRate           resource.Rational          `json:"target_rate"`
	TimeUnit             string                     `json:"time_unit"`
	Mode                 SolveMode                  `json:"mode"`
	StopPoints           map[string]bool            `json:"stop_points"`
	RecipeOverrides      map[string]string          `json:"recipe_overrides"`
	TagOverrides         map[string]string          `json:"tag_overrides"` // tagName → "mod_id:item_id"
	FactoryState         FactoryState               `json:"factory_state"`
	AllowPartialMachines []string                   `json:"allow_partial_machines"` // groups that may stand idle, by RateKey
	ModConfigs           map[string]json.RawMessage `json:"mod_configs"`
	// Factor multiplies the solver's answer; the zero value means x1.
	Factor resource.Rational `json:"factor"`
	// VariantPins fixes a machine group's variant, keyed by RecipeOptionKey.
	VariantPins map[string]string `json:"variant_pins"`
}

// TagResolution describes how a tag was resolved during solving.
type TagResolution struct {
	Chosen  resource.Ref   `json:"chosen"`
	Options []resource.Ref `json:"options"`
}

type FactoryState struct {
	ExistingOutputs map[string]resource.Rational `json:"existing_outputs"`
}

// Warning is a structured, translatable solver warning.
type Warning struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

type SolveResult struct {
	MachineGroups  []MachineGroupDraft      `json:"machine_groups"`
	IOProfile      IOProfile                `json:"io_profile"`
	ActualRate     resource.Rational        `json:"actual_rate"`
	HadCycles      bool                     `json:"had_cycles"`
	ModeUsed       SolveMode                `json:"mode_used"`
	Warnings       []Warning                `json:"warnings"`
	TagResolutions map[string]TagResolution `json:"tag_resolutions"` // tagKey → resolution
}

type MachineRef struct {
	ModID     string
	MachineID string
}

type MachineGroupDraft struct {
	MachineMod   string            `json:"machine_mod"`
	MachineID    string            `json:"machine_id"`
	RecipeID     string            `json:"recipe_id"`
	RecipeOutput resource.Ref      `json:"recipe_output"` // item this recipe produces
	Count        int64             `json:"count"`
	ExactCount   resource.Rational `json:"exact_count"` // fractional machine count before ceiling
	Utilization  resource.Rational `json:"utilization"`
	Status       DraftStatus       `json:"status"`
	// Variant is the chosen operating configuration, VariantID/Label/Costs its summary.
	VariantID string `json:"variant_id"`
	Label     string `json:"label"`
	// PluginMod is the mod whose plugin evaluated this group.
	PluginMod string          `json:"plugin_mod"`
	Costs     []plugins.Cost  `json:"costs"`
	Variant   plugins.Variant `json:"variant"`
	// VariantOptions lists every runnable variant of this group.
	VariantOptions []VariantOption `json:"variant_options"`
	// RateKey names the node this group was computed for.
	RateKey string `json:"rate_key"`
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

// VariantOption is one selectable operating variant of a machine group.
type VariantOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type IOProfile struct {
	Inputs  []IOEntry `json:"inputs"`
	Outputs []IOEntry `json:"outputs"`
}

type IOEntry struct {
	Item              resource.Ref      `json:"ref"`
	Rate              resource.Rational `json:"rate"`
	TimeUnit          string            `json:"time_unit"`
	IsStopPoint       bool              `json:"is_stop_point"`
	IsFactoryProvided bool              `json:"is_factory_provided"`
}

// ErrCycleBreakNeeded is returned by Solve for a cycle it cannot resolve itself.
type ErrCycleBreakNeeded struct {
	CycleNodes []string // ItemRef.Key() values of stuck nodes
}

func (e *ErrCycleBreakNeeded) Error() string {
	return fmt.Sprintf("cycle break needed: %v", e.CycleNodes)
}
