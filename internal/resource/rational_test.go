package resource

import (
	"errors"
	"testing"
)

func TestRationalAdd(t *testing.T) {
	cases := []struct{ a, b, want Rational }{
		{Rational{1, 2}, Rational{1, 3}, Rational{5, 6}},
		{Rational{1, 4}, Rational{3, 4}, Rational{1, 1}},
		{Rational{-1, 3}, Rational{1, 3}, Rational{0, 1}},
		{Rational{2, 6}, Rational{3, 9}, Rational{2, 3}}, // both reduce to 1/3 → 2/3
		{Rational{0, 1}, Rational{5, 7}, Rational{5, 7}},
	}
	for _, c := range cases {
		got := c.a.Add(c.b)
		if got.Num != c.want.Num || got.Den != c.want.Den {
			t.Errorf("(%d/%d)+(%d/%d) = %d/%d, want %d/%d",
				c.a.Num, c.a.Den, c.b.Num, c.b.Den,
				got.Num, got.Den, c.want.Num, c.want.Den)
		}
	}
}

func TestRationalMul(t *testing.T) {
	cases := []struct{ a, b, want Rational }{
		{Rational{2, 3}, Rational{3, 4}, Rational{1, 2}},
		{Rational{1, 1}, Rational{7, 5}, Rational{7, 5}},
		{Rational{0, 1}, Rational{99, 7}, Rational{0, 1}},
		{Rational{-1, 2}, Rational{2, 1}, Rational{-1, 1}},
	}
	for _, c := range cases {
		got := c.a.Mul(c.b)
		if got.Num != c.want.Num || got.Den != c.want.Den {
			t.Errorf("(%d/%d)*(%d/%d) = %d/%d, want %d/%d",
				c.a.Num, c.a.Den, c.b.Num, c.b.Den,
				got.Num, got.Den, c.want.Num, c.want.Den)
		}
	}
}

func TestNewRationalReduces(t *testing.T) {
	cases := []struct{ n, d, wn, wd int64 }{
		{6, 4, 3, 2},
		{-4, 6, -2, 3},
		{0, 7, 0, 1},
		{5, 5, 1, 1},
	}
	for _, c := range cases {
		got := NewRational(c.n, c.d)
		if got.Num != c.wn || got.Den != c.wd {
			t.Errorf("NewRational(%d,%d) = %d/%d, want %d/%d",
				c.n, c.d, got.Num, got.Den, c.wn, c.wd)
		}
	}
}

func TestRationalGCD(t *testing.T) {
	cases := []struct{ a, b, want uint64 }{
		{12, 8, 4},
		{100, 75, 25},
		{7, 1, 1},
		{0, 5, 5},
	}
	for _, c := range cases {
		got := ugcd(c.a, c.b)
		if got != c.want {
			t.Errorf("ugcd(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestRateArithmeticErrorClassifies(t *testing.T) {
	cases := []struct {
		panicValue any
		want       error
	}{
		{"rational: int64 overflow", ErrRateOverflow},
		{"LCM: MinInt64 unsupported", ErrRateDomain},
		{"rational: division by zero", ErrRateDomain},
		{"rational: zero denominator", ErrRateDomain},
		{"runtime error: index out of range [3]", nil},
		{errors.New("rational: int64 overflow"), nil},
	}
	for _, c := range cases {
		got := rateArithmeticError(c.panicValue)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("rateArithmeticError(%v) = %v, want nil so the caller re-panics", c.panicValue, got)
		case c.want != nil && !errors.Is(got, c.want):
			t.Errorf("rateArithmeticError(%v) = %v, want %v", c.panicValue, got, c.want)
		}
	}
}

// The exported wrapper keeps the same classification: rate arithmetic becomes an error,
// anything else stays a panic.
func TestGuardRateArithmeticClassifiesOnlyRateArithmetic(t *testing.T) {
	t.Run("overflow becomes an error", func(t *testing.T) {
		var err error
		func() {
			defer GuardRateArithmetic(&err)
			panic("rational: int64 overflow")
		}()
		if !errors.Is(err, ErrRateOverflow) {
			t.Errorf("err = %v, want it to wrap ErrRateOverflow", err)
		}
	})

	t.Run("domain error becomes an error", func(t *testing.T) {
		var err error
		func() {
			defer GuardRateArithmetic(&err)
			panic("rational: zero denominator")
		}()
		if !errors.Is(err, ErrRateDomain) {
			t.Errorf("err = %v, want it to wrap ErrRateDomain", err)
		}
	})

	t.Run("a bug still panics", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("a non-arithmetic panic was swallowed; it must propagate")
			}
			if got, ok := r.(string); !ok || got != "runtime error: index out of range" {
				t.Errorf("re-panicked with %v, want the original value unchanged", r)
			}
		}()
		var err error
		defer GuardRateArithmetic(&err)
		panic("runtime error: index out of range")
	})
}
