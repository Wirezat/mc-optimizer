package fracidx

// alpha is the ordered character set for position keys.
// It is a subset of ASCII in strictly ascending code-point order,
// so Go's byte-wise string comparison matches the intended numeric ordering.
const alpha = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const n = len(alpha) // 62

// charValue maps ASCII byte values to their index in alpha (O(1) lookup).
// Bytes not in alpha map to 0 and should never appear in valid position keys.
var charValue [128]int

func init() {
	for i := range []byte(alpha) {
		charValue[alpha[i]] = i
	}
}

// Initial returns the starting key for an empty list (middle of the alphabet).
func Initial() string { return string(alpha[n/2]) } // "V"

// Between returns a key k such that a < k < b lexicographically.
//   - a="" means no lower bound (k will be < b).
//   - b="" means no upper bound (k will be > a).
//   - Both empty returns Initial().
//
// The algorithm compares a and b character by character, finds the first
// position where they differ, and picks a character in the gap.  When
// neighbours are adjacent (gap of 1), it takes the lower character and
// recurses on the remainder of a with no upper bound, always terminating
// because the recursion eventually reaches a gap ≥ 2 or an empty string.
//
// Invariant: the returned key never ends with alpha[0] ('0').
func Between(a, b string) string {
	if a == "" && b == "" {
		return Initial()
	}
	return string(betweenBytes(a, b))
}

// betweenBytes is the inner implementation; it returns the result as []byte
// to allow callers to build the output incrementally without extra allocations.
func betweenBytes(a, b string) []byte {
	var out []byte
	for i := 0; ; i++ {
		// ai: digit at position i of a; 0 (minimum) when a is exhausted.
		ai := 0
		if i < len(a) {
			ai = charValue[a[i]]
		}

		// bi: digit at position i of b; n (past maximum) when b provides
		// no upper bound at this position (b="" or b is exhausted).
		bi := n
		if b != "" && i < len(b) {
			bi = charValue[b[i]]
		}

		switch {
		case ai == bi:
			// Characters match — copy and keep walking.
			out = append(out, alpha[ai])

		case bi-ai >= 2:
			// There is a character strictly between ai and bi — use the midpoint.
			out = append(out, alpha[(ai+bi)/2])
			return out

		default:
			// bi-ai == 1: neighbours with no character between them.
			// Commit to the lower value, then extend by finding something
			// strictly after the remainder of a (no upper bound).
			out = append(out, alpha[ai])
			aSuffix := ""
			if i+1 < len(a) {
				aSuffix = a[i+1:]
			}
			return append(out, betweenBytes(aSuffix, "")...)
		}
	}
}
