package fracidx_test

import (
	"fmt"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/fracidx"
)

// assertBetween fails the test if mid is not strictly between a and b.
// Empty string means "unbounded": a="" means no lower bound, b="" means no upper bound.
func assertBetween(t *testing.T, label, a, mid, b string) {
	t.Helper()
	if a != "" && mid <= a {
		t.Errorf("%s: Between(%q,%q)=%q — not > a", label, a, b, mid)
	}
	if b != "" && mid >= b {
		t.Errorf("%s: Between(%q,%q)=%q — not < b", label, a, b, mid)
	}
	if mid == "" {
		t.Errorf("%s: Between(%q,%q) returned empty string", label, a, b)
	}
}

func TestInitial(t *testing.T) {
	v := fracidx.Initial()
	if v != "V" {
		t.Errorf("Initial()=%q want %q", v, "V")
	}
}

func TestBetweenBothEmpty(t *testing.T) {
	got := fracidx.Between("", "")
	if got != fracidx.Initial() {
		t.Errorf("Between('','')=%q want %q", got, fracidx.Initial())
	}
}

func TestBetweenAfterOnly(t *testing.T) {
	// Between(a, "") must always be > a
	cases := []string{"V", "0", "z", "V0001000", "V0001000V", "aZ9"}
	for _, a := range cases {
		got := fracidx.Between(a, "")
		if got <= a {
			t.Errorf("Between(%q,'')=%q — not > a", a, got)
		}
	}
}

func TestBetweenBeforeOnly(t *testing.T) {
	// Between("", b) must always be < b
	cases := []string{"V", "z", "0001", "V0001000", "B"}
	for _, b := range cases {
		got := fracidx.Between("", b)
		if got >= b {
			t.Errorf("Between('',%q)=%q — not < b", b, got)
		}
	}
}

func TestBetweenKnownPairs(t *testing.T) {
	pairs := [][2]string{
		{"V", "W"},
		{"V", "V1"},
		{"V0", "V1"},
		{"V0001000", "V0002000"},
		{"V0001000", "V0001001"},
		{"0", "z"},
		{"A", "B"},
		{"a", "b"},
		{"V0000001", "V0000002"},
	}
	for _, p := range pairs {
		a, b := p[0], p[1]
		mid := fracidx.Between(a, b)
		assertBetween(t, fmt.Sprintf("Between(%q,%q)", a, b), a, mid, b)
	}
}

// TestInsertAtBeginning simulates a user always prepending to a list.
func TestInsertAtBeginning(t *testing.T) {
	positions := []string{fracidx.Initial()}
	for i := range 500 {
		first := positions[0]
		pos := fracidx.Between("", first)
		if pos >= first {
			t.Fatalf("iter %d: Between('',%q)=%q — not < first", i, first, pos)
		}
		positions = append([]string{pos}, positions...)
	}
	assertSorted(t, "insert-at-beginning", positions)
	t.Logf("max key length after 500 prepends: %d", maxLen(positions))
}

// TestInsertAtEnd simulates a user always appending to a list.
func TestInsertAtEnd(t *testing.T) {
	positions := []string{fracidx.Initial()}
	for i := range 500 {
		last := positions[len(positions)-1]
		pos := fracidx.Between(last, "")
		if pos <= last {
			t.Fatalf("iter %d: Between(%q,'')=%q — not > last", i, last, pos)
		}
		positions = append(positions, pos)
	}
	assertSorted(t, "insert-at-end", positions)
	t.Logf("max key length after 500 appends: %d", maxLen(positions))
}

// TestInsertInMiddle simulates always inserting in the middle of the list.
func TestInsertInMiddle(t *testing.T) {
	positions := []string{fracidx.Between("", ""), fracidx.Between(fracidx.Initial(), "")}
	for i := range 500 {
		mid := len(positions) / 2
		a := positions[mid-1]
		b := positions[mid]
		pos := fracidx.Between(a, b)
		assertBetween(t, fmt.Sprintf("iter %d", i), a, pos, b)
		// Insert at mid
		positions = append(positions[:mid+1], positions[mid:]...)
		positions[mid] = pos
	}
	assertSorted(t, "insert-in-middle", positions)
	t.Logf("max key length after 500 middle-inserts: %d", maxLen(positions))
}

// TestMigrationInitValues verifies the format the SQL migration produces.
func TestMigrationInitValues(t *testing.T) {
	// Reproduce: 'V' || LPAD((rn * 1000)::text, 7, '0')
	var positions []string
	for i := range 100 {
		positions = append(positions, fmt.Sprintf("V%07d", (i+1)*1000))
	}
	assertSorted(t, "migration-init", positions)

	// Between adjacent migration values must work
	for i := 1; i < len(positions); i++ {
		a, b := positions[i-1], positions[i]
		mid := fracidx.Between(a, b)
		assertBetween(t, fmt.Sprintf("migration pair %d", i), a, mid, b)
	}
}

// TestTightGap verifies that even adjacent single-digit positions can be split.
func TestTightGap(t *testing.T) {
	// These differ by only 1 in the last digit
	pairs := [][2]string{
		{"V0001000", "V0001001"},
		{"VVVVVVVV", "VVVVVVVa"}, // 'a' follows 'Z' in our alphabet
		{"0", "1"},
		{"A", "B"},
	}
	for _, p := range pairs {
		a, b := p[0], p[1]
		mid := fracidx.Between(a, b)
		assertBetween(t, fmt.Sprintf("tight(%q,%q)", a, b), a, mid, b)
	}
}

// TestMultiStepAtSameGap stresses the same gap repeatedly — key must grow
// but always remain valid.
func TestMultiStepAtSameGap(t *testing.T) {
	a := "V0001000"
	b := "V0001001"
	prev := a
	for i := range 20 {
		mid := fracidx.Between(prev, b)
		assertBetween(t, fmt.Sprintf("same-gap iter %d", i), prev, mid, b)
		prev = mid
	}
	t.Logf("final key after 20 same-gap insertions: %q (len=%d)", prev, len(prev))
}

// TestKeyLengthStaysReasonable checks that repeated end-insertions don't produce
// unbounded strings.  The algorithm grows at roughly 1 character per 6 appends
// (the base-62 alphabet exhausts in 6 steps: V→k→s→w→y→z→zV…), so 1000
// appends yields ≈167 chars.  We verify both a short-run (100 appends → <30)
// and a long-run (1000 appends → <200) to catch regressions at either scale.
func TestKeyLengthStaysReasonable(t *testing.T) {
	last := fracidx.Initial()
	for i := range 1000 {
		last = fracidx.Between(last, "")
		if i == 99 && len(last) > 30 {
			t.Errorf("key too long after 100 appends: len=%d key=%q", len(last), last)
		}
	}
	if len(last) > 200 {
		t.Errorf("key too long after 1000 appends: len=%d key=%q", len(last), last)
	}
	t.Logf("key after 1000 appends: %q (len=%d)", last, len(last))
}

// TestLexicographicConsistency verifies that string comparison order matches
// the order in which positions were produced.
func TestLexicographicConsistency(t *testing.T) {
	// Build a list of 200 items via alternating inserts
	positions := []string{fracidx.Initial()}
	for i := range 199 {
		switch i % 4 {
		case 0:
			positions = append([]string{fracidx.Between("", positions[0])}, positions...)
		case 1:
			positions = append(positions, fracidx.Between(positions[len(positions)-1], ""))
		case 2, 3:
			mid := len(positions) / 2
			a := ""
			if mid > 0 {
				a = positions[mid-1]
			}
			p := fracidx.Between(a, positions[mid])
			positions = append(positions[:mid+1], positions[mid:]...)
			positions[mid] = p
		}
	}
	assertSorted(t, "mixed-inserts", positions)
}

// assertSorted fails if positions is not strictly ascending.
func assertSorted(t *testing.T, label string, positions []string) {
	t.Helper()
	for i := 1; i < len(positions); i++ {
		if positions[i-1] >= positions[i] {
			t.Errorf("%s: not sorted at [%d]: %q >= %q", label, i, positions[i-1], positions[i])
		}
	}
}

func maxLen(positions []string) int {
	m := 0
	for _, p := range positions {
		if len(p) > m {
			m = len(p)
		}
	}
	return m
}
