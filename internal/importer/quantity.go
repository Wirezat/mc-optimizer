package importer

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"gopkg.in/yaml.v3"
)

// exactNumber is a YAML scalar read as an exact fraction, written as "2", "0.66" or "1/3".
type exactNumber struct {
	num, den int
}

func (e *exactNumber) UnmarshalYAML(n *yaml.Node) error {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(n.Value))
	if !ok || n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: %q is not a number", n.Line, n.Value)
	}
	if !fitsInt32(r.Num().Int64(), r.Num().IsInt64()) || !fitsInt32(r.Denom().Int64(), r.Denom().IsInt64()) {
		return fmt.Errorf("line %d: %q is out of range", n.Line, n.Value)
	}
	e.num, e.den = int(r.Num().Int64()), int(r.Denom().Int64())
	return nil
}

func fitsInt32(v int64, ok bool) bool {
	return ok && v >= math.MinInt32 && v <= math.MaxInt32
}

// checkAmount rejects an amount that is not a positive fraction.
func checkAmount(num, den int64) error {
	if den <= 0 {
		return fmt.Errorf("amount %d/%d has a non-positive denominator", num, den)
	}
	if num <= 0 {
		return fmt.Errorf("amount %d/%d must be positive", num, den)
	}
	return nil
}

// checkProbability rejects a probability outside [0, 1], and zero unless allowZero.
func checkProbability(num, den int64, allowZero bool) error {
	if den <= 0 {
		return fmt.Errorf("probability %d/%d has a non-positive denominator", num, den)
	}
	if num < 0 || num > den {
		return fmt.Errorf("probability %d/%d must lie between 0 and 1", num, den)
	}
	if num == 0 && !allowZero {
		return fmt.Errorf("probability of an output must be above 0")
	}
	return nil
}
