// Package plugins runs mod-owned JavaScript plugins in-process and mirrors their wire types
// into Go.
package plugins

import "encoding/json"

// Rational is an exact fraction.
type Rational struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

// Cost is a resource consumption per tick.
type Cost struct {
	Resource string   `json:"resource"`
	Amount   Rational `json:"amount"`
}

// Output is an effective recipe output per craft.
type Output struct {
	Ref         string   `json:"ref"`
	Amount      Rational `json:"amount"`
	Probability Rational `json:"probability"`
}

// Item is an installed upgrade item of a variant.
type Item struct {
	Ref   string `json:"ref"`
	Count int    `json:"count"`
}

// Variant is an evaluated operating configuration for a (machine, recipe) pair.
type Variant struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Rate    Rational `json:"rate"`
	Costs   []Cost   `json:"costs"`
	Outputs []Output `json:"outputs"`
	Items   []Item   `json:"items"`
	Valid   bool     `json:"valid"`
	// Rank breaks a tie within one plugin, smaller first.
	Rank int `json:"rank"`
}

// EvalMachine is the plugin-facing view of a machine.
type EvalMachine struct {
	ModID     string          `json:"mod_id"`
	MachineID string          `json:"machine_id"`
	Data      json.RawMessage `json:"data"`
}

// EvalRecipe is the plugin-facing view of a recipe.
type EvalRecipe struct {
	ID string `json:"id"`
	// The recipe's own machine, not the evaluated one on a machine_interfaces recipe.
	MachineMod    string          `json:"machine_mod"`
	MachineID     string          `json:"machine_id"`
	DurationTicks int64           `json:"duration_ticks"`
	Inputs        []Output        `json:"inputs"`
	Outputs       []Output        `json:"outputs"`
	Data          json.RawMessage `json:"data"`
}

// EvalContext is the single argument passed to evaluate(ctx).
type EvalContext struct {
	Machine EvalMachine     `json:"machine"`
	Recipe  EvalRecipe      `json:"recipe"`
	Config  json.RawMessage `json:"config"`
}
