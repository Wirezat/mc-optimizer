package solver

// RecipeRow is the lean recipe representation exchanged between the DB and solver.
// Only the fields needed for rate calculations are present.
type RecipeRow struct {
	ID            string
	MachineMod    string
	MachineID     string
	DurationTicks int
	EUPerTick     int64
	TotalEU       int64
	ItemInputs    []RecipeRowItemIO
	ItemOutputs   []RecipeRowItemIO
	FluidInputs   []RecipeRowFluidIO
	FluidOutputs  []RecipeRowFluidIO
}

type RecipeRowItemIO struct {
	ItemModID      *string
	ItemID         *string
	TagID          *string
	TagName        *string // resolved from the tags table
	AmountNum      int64
	AmountDen      int64
	ProbabilityNum int64
	ProbabilityDen int64
	NonConsuming   bool // true = reusable tool; not factored into consumption rates
}

type RecipeRowFluidIO struct {
	FluidModID     string
	FluidID        string
	AmountMB       int64
	ProbabilityNum int64
	ProbabilityDen int64
}

// MachineSpec holds the subset of machine properties the solver needs.
type MachineSpec struct {
	ModID         string
	MachineID     string
	EnergyType    string
	BaseEUPerTick int64
	MaxEUPerTick  int64
	MaxSlots      int16
	Upgradable    bool
}

// UpgradeTierSpec holds the subset of upgrade tier properties the solver needs.
type UpgradeTierSpec struct {
	ID             string
	EUBonusPerSlot int64
}
