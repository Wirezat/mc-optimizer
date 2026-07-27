package solver

import (
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
