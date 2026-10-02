// Package plugins runs mod-owned JavaScript plugins in-process; these are their wire types.
package plugins

import "encoding/json"

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

// Input is a recipe input as a plugin sees it; Consumed is false for a tool.
type Input struct {
	Ref         string   `json:"ref"`
	Amount      Rational `json:"amount"`
	Probability Rational `json:"probability"`
	Consumed    bool     `json:"consumed"`
}

type Item struct {
	Ref   string `json:"ref"`
	Count int    `json:"count"`
}

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

type EvalMachine struct {
	ModID     string          `json:"mod_id"`
	MachineID string          `json:"machine_id"`
	Data      json.RawMessage `json:"data"`
}

type EvalRecipe struct {
	ID string `json:"id"`
	// The recipe's own machine, not the evaluated one on a machine_interfaces recipe.
	MachineMod    string          `json:"machine_mod"`
	MachineID     string          `json:"machine_id"`
	DurationTicks int64           `json:"duration_ticks"`
	Inputs        []Input         `json:"inputs"`
	Outputs       []Output        `json:"outputs"`
	Data          json.RawMessage `json:"data"`
}

type EvalContext struct {
	Machine EvalMachine     `json:"machine"`
	Recipe  EvalRecipe      `json:"recipe"`
	Config  json.RawMessage `json:"config"`
}
