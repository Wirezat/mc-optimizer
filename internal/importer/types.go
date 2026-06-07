package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// MIRecipe is the raw JSON shape of a Modern Industrialization datapack recipe.
// item_inputs/outputs and fluid_inputs/outputs can be a single object or an array;
// RawIOList handles both via a custom unmarshaler.
type MIRecipe struct {
	Type         string    `json:"type"`
	EU           int64     `json:"eu"`
	Duration     int       `json:"duration"`
	ItemInputs   RawIOList `json:"item_inputs"`
	ItemOutputs  RawIOList `json:"item_outputs"`
	FluidInputs  RawIOList `json:"fluid_inputs"`
	FluidOutputs RawIOList `json:"fluid_outputs"`

	// SourceFile is set by the caller and is not part of the JSON schema.
	SourceFile string `json:"-"`
}

// RawIO is one entry in an item_inputs / item_outputs / fluid_inputs / fluid_outputs list.
type RawIO struct {
	Item        string  `json:"item"`        // "mod:id" — mutually exclusive with Tag
	Tag         string  `json:"tag"`         // "forge:tag" — mutually exclusive with Item
	Fluid       string  `json:"fluid"`       // "mod:id" for fluid entries
	Amount      int     `json:"amount"`
	Probability float64 `json:"probability"` // defaults to 1.0 if absent
}

// RawIOList unmarshals both a single RawIO object and a JSON array of them.
type RawIOList []RawIO

func (r *RawIOList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '[' {
		var arr []RawIO
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*r = arr
		return nil
	}
	var obj RawIO
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*r = RawIOList{obj}
	return nil
}

// SplitTypeField splits "mod_id:machine_id" into its two components.
func SplitTypeField(s string) (modID, machineID string, err error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid type %q: expected mod_id:machine_id", s)
	}
	return parts[0], parts[1], nil
}

// ProbToRational converts a float64 probability to an exact integer rational (num/den).
// Tries denominators up to 10 000 for an exact match; falls back to rounding.
func ProbToRational(p float64) (num, den int) {
	if p <= 0 {
		return 0, 1
	}
	if p >= 1 {
		return 1, 1
	}
	for d := 1; d <= 10_000; d++ {
		n := int(math.Round(p * float64(d)))
		if math.Abs(float64(n)/float64(d)-p) < 1e-9 {
			g := gcd(n, d)
			return n / g, d / g
		}
	}
	n := int(math.Round(p * 10_000))
	g := gcd(n, 10_000)
	return n / g, 10_000 / g
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
