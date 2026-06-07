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

type ItemRef struct {
	ModID  string
	ItemID string
}

// Key returns the canonical string key for use in maps.
func (i *ItemRef) Key() string {
	return i.ModID + ":" + i.ItemID
}

type SolveRequest struct {
	TargetItem      ItemRef
	TargetRate      Rational
	TimeUnit        string
	Mode            SolveMode
	StopPoints      map[string]bool
	RecipeOverrides map[string]string
	FactoryState    FactoryState
}

type FactoryState struct {
	ExistingOutputs map[string]Rational
}

type SolveResult struct {
	MachineGroups []MachineGroupDraft
	IOProfile     IOProfile
	ActualRate    Rational
	HadCycles     bool
	ModeUsed      SolveMode
	Warnings      []string
}

type MachineGroupDraft struct {
	MachineMod   string
	MachineID    string
	RecipeID     string
	Count        int64
	Utilization  Rational
	UpgradeTier  string
	UpgradeCount int
	Status       DraftStatus
}

type IOProfile struct {
	Inputs  []IOEntry
	Outputs []IOEntry
}

type IOEntry struct {
	Item        ItemRef
	Rate        Rational
	TimeUnit    string
	IsStopPoint bool
}

type DraftPayload struct {
	Request SolveRequest `json:"request"`
	Result  SolveResult  `json:"result"`
}
