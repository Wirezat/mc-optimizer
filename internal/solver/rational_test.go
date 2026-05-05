package solver

import "testing"

// mustPanic asserts that f panics. Fails the test if it does not.
func mustPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected panic, but none occurred")
		}
	}()
	f()
}

// --- NewRational ---

func TestNewRational_Reduces(t *testing.T) {
	r := NewRational(6, 4)
	if r.Num != 3 || r.Den != 2 {
		t.Errorf("expected 3/2, got %v", r)
	}
}

func TestNewRational_NegativeDenominator(t *testing.T) {
	r := NewRational(3, -4)
	if r.Num != -3 || r.Den != 4 {
		t.Errorf("expected -3/4, got %v", r)
	}
}

func TestNewRational_ZeroDenominatorPanics(t *testing.T) {
	mustPanic(t, func() { NewRational(1, 0) })
}

func TestNewRational_Zero(t *testing.T) {
	r := NewRational(0, 5)
	if r.Num != 0 || r.Den != 1 {
		t.Errorf("expected 0/1, got %v", r)
	}
}

// --- Arithmetic ---

func TestAdd(t *testing.T) {
	cases := []struct {
		a, b, want Rational
	}{
		{NewRational(1, 2), NewRational(1, 3), NewRational(5, 6)},
		{NewRational(-1, 2), NewRational(1, 2), NewRational(0, 1)},
		{NewRational(1, 4), NewRational(3, 4), RationalFromInt(1)},
	}
	for _, c := range cases {
		got := c.a.Add(c.b)
		if got.Cmp(c.want) != 0 {
			t.Errorf("%v + %v = %v, expected %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSub(t *testing.T) {
	cases := []struct {
		a, b, want Rational
	}{
		{NewRational(3, 4), NewRational(1, 4), NewRational(1, 2)},
		{NewRational(1, 2), NewRational(1, 2), NewRational(0, 1)},
		{NewRational(1, 3), NewRational(1, 2), NewRational(-1, 6)},
	}
	for _, c := range cases {
		got := c.a.Sub(c.b)
		if got.Cmp(c.want) != 0 {
			t.Errorf("%v - %v = %v, expected %v", c.a, c.b, got, c.want)
		}
	}
}

func TestMul(t *testing.T) {
	cases := []struct {
		a, b, want Rational
	}{
		{NewRational(3, 4), NewRational(2, 5), NewRational(3, 10)},
		{NewRational(-1, 2), NewRational(2, 3), NewRational(-1, 3)},
		{NewRational(0, 1), NewRational(5, 7), NewRational(0, 1)},
	}
	for _, c := range cases {
		got := c.a.Mul(c.b)
		if got.Cmp(c.want) != 0 {
			t.Errorf("%v * %v = %v, expected %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDiv(t *testing.T) {
	cases := []struct {
		a, b, want Rational
	}{
		{NewRational(1, 2), NewRational(1, 4), RationalFromInt(2)},
		{NewRational(3, 4), NewRational(3, 4), RationalFromInt(1)},
		{NewRational(-1, 2), NewRational(1, 4), RationalFromInt(-2)},
	}
	for _, c := range cases {
		got := c.a.Div(c.b)
		if got.Cmp(c.want) != 0 {
			t.Errorf("%v / %v = %v, expected %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDiv_ByZeroPanics(t *testing.T) {
	mustPanic(t, func() { NewRational(1, 2).Div(NewRational(0, 1)) })
}

// --- Comparison & predicates ---

func TestCmp(t *testing.T) {
	cases := []struct {
		a, b Rational
		want int
	}{
		{NewRational(1, 3), NewRational(1, 2), -1},
		{NewRational(1, 2), NewRational(1, 2), 0},
		{NewRational(2, 3), NewRational(1, 2), 1},
		{NewRational(-1, 2), NewRational(1, 2), -1},
	}
	for _, c := range cases {
		got := c.a.Cmp(c.b)
		if got != c.want {
			t.Errorf("Cmp(%v, %v) = %d, expected %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsZero(t *testing.T) {
	if !NewRational(0, 5).IsZero() {
		t.Error("0/5 should be zero")
	}
	if NewRational(1, 5).IsZero() {
		t.Error("1/5 should not be zero")
	}
}

func TestIsNegative(t *testing.T) {
	if !NewRational(-1, 2).IsNegative() {
		t.Error("-1/2 should be negative")
	}
	if NewRational(1, 2).IsNegative() {
		t.Error("1/2 should not be negative")
	}
	if NewRational(0, 1).IsNegative() {
		t.Error("0 should not be negative")
	}
}

// --- CeilInt ---

func TestCeilInt(t *testing.T) {
	cases := []struct {
		num, den int64
		want     int64
	}{
		{4, 2, 2},   // exact
		{5, 2, 3},   // round up
		{1, 3, 1},   // round up to 1
		{6, 3, 2},   // exact
		{-3, 2, -1}, // ceil(-1.5) = -1; Go truncation gives -1 directly
		{-4, 2, -2}, // ceil(-2.0) = -2; exact negative
		{-1, 3, 0},  // ceil(-0.33) = 0
	}
	for _, c := range cases {
		r := NewRational(c.num, c.den)
		if got := r.CeilInt(); got != c.want {
			t.Errorf("ceil(%v) = %d, expected %d", r, got, c.want)
		}
	}
}

// --- LCM ---

func TestLCM(t *testing.T) {
	cases := []struct {
		input []int64
		want  int64
	}{
		{[]int64{2, 3, 4}, 12},
		{[]int64{5}, 5},
		{[]int64{}, 1},
		{[]int64{6, 4}, 12},
		{[]int64{7, 13}, 91}, // two primes
	}
	for _, c := range cases {
		got := LCM(c.input)
		if got != c.want {
			t.Errorf("LCM(%v) = %d, expected %d", c.input, got, c.want)
		}
	}
}

// --- ConvertToPerTick ---

func TestConvertToPerTick(t *testing.T) {
	rate := RationalFromInt(10)
	cases := []struct {
		unit string
		want Rational
	}{
		{"t", rate},                    // already per-tick
		{"s", NewRational(10, 20)},     // 10/s ÷ 20
		{"min", NewRational(10, 1200)}, // 10/min ÷ 1200
		{"h", NewRational(10, 72000)},  // 10/h ÷ 72000
	}
	for _, c := range cases {
		got := ConvertToPerTick(rate, c.unit)
		if got.Cmp(c.want) != 0 {
			t.Errorf("ConvertToPerTick(10, %q) = %v, expected %v", c.unit, got, c.want)
		}
	}
}

func TestConvertToPerTick_UnknownUnitPanics(t *testing.T) {
	mustPanic(t, func() { ConvertToPerTick(RationalFromInt(1), "week") })
}

// --- String ---

func TestString(t *testing.T) {
	cases := []struct {
		r    Rational
		want string
	}{
		{RationalFromInt(3), "3"},
		{NewRational(1, 2), "1/2"},
		{NewRational(-3, 4), "-3/4"},
	}
	for _, c := range cases {
		if got := c.r.String(); got != c.want {
			t.Errorf("String(%v) = %q, expected %q", c.r, got, c.want)
		}
	}
}
