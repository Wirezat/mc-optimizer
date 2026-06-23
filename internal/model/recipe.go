package model

type Recipe struct {
	ID            string  `json:"id"`
	MachineModID  string  `json:"machine_mod_id"`
	MachineID     string  `json:"machine_id"`
	MachineName   *string `json:"machine_name,omitempty"`
	Name          *string `json:"name,omitempty"`
	DurationTicks int     `json:"duration_ticks"`
	EUPerTick     int64   `json:"eu_per_tick"`
	TotalEU       int64   `json:"total_eu"`
	Shape         []string `json:"shape,omitempty"`

	ItemInputs   []RecipeItemIO `json:"item_inputs"`
	ItemOutputs  []RecipeItemIO `json:"item_outputs"`
	FluidInputs  []RecipeFluidIO `json:"fluid_inputs"`
	FluidOutputs []RecipeFluidIO `json:"fluid_outputs"`
}

type RecipeItemIO struct {
	ID             string  `json:"id"`
	RecipeID       string  `json:"recipe_id"`
	ItemModID      *string `json:"item_mod_id,omitempty"`
	ItemID         *string `json:"item_id,omitempty"`
	ItemName       *string `json:"item_name,omitempty"`
	TagID          *string `json:"tag_id,omitempty"`
	TagName        *string `json:"tag_name,omitempty"`
	AmountNum      int     `json:"amount_num"`
	AmountDen      int     `json:"amount_den"`
	ProbabilityNum int     `json:"probability_num"`
	ProbabilityDen int     `json:"probability_den"`
	NonConsuming   bool    `json:"non_consuming,omitempty"`
}

type RecipeFluidIO struct {
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

	ItemInputs   []CreateRecipeItemIO `json:"item_inputs"`
	ItemOutputs  []CreateRecipeItemIO `json:"item_outputs"`
	FluidInputs  []CreateRecipeFluidIO `json:"fluid_inputs"`
	FluidOutputs []CreateRecipeFluidIO `json:"fluid_outputs"`
}

type CreateRecipeItemIO struct {
	ItemModID      *string `json:"item_mod_id"`
	ItemID         *string `json:"item_id"`
	TagID          *string `json:"tag_id"`
	AmountNum      int     `json:"amount_num"`
	AmountDen      int     `json:"amount_den"`
	ProbabilityNum int     `json:"probability_num"`
	ProbabilityDen int     `json:"probability_den"`
}

type CreateRecipeFluidIO struct {
	FluidModID     string `json:"fluid_mod_id"`
	FluidID        string `json:"fluid_id"`
	AmountMB       int64  `json:"amount_mb"`
	ProbabilityNum int    `json:"probability_num"`
	ProbabilityDen int    `json:"probability_den"`
}
