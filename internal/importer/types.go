package importer

import (
	"fmt"
	"math"
	"strings"
)

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
