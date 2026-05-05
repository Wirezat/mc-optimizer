package solver

import "fmt"

// Rational is an exact rational number represented as a reduced fraction num/den.
// No float64 in the solver — all calculations go through this type.
//
// Invariants:
//   - Den is always > 0 (sign lives in Num)
//   - Num and Den are always coprime (reduced on construction)
//
// Known limitation: arithmetic operations use int64 multiplication internally.
// Overflow is possible with very large numerators/denominators.
// Use small, pre-reduced inputs to stay safe.
type Rational struct {
	Num int64
	Den int64
}

// NewRational returns the reduced fraction num/den.
// Panics if den == 0.
func NewRational(num, den int64) Rational {
	if den == 0 {
		panic("rational: denominator must not be zero")
	}
	if den < 0 {
		num, den = -num, -den
	}
	g := gcd(abs64(num), abs64(den))
	return Rational{Num: num / g, Den: den / g}
}

// RationalFromInt returns n/1.
func RationalFromInt(n int64) Rational {
	return Rational{Num: n, Den: 1}
}

func (r Rational) Add(o Rational) Rational {
	return NewRational(r.Num*o.Den+o.Num*r.Den, r.Den*o.Den)
}

func (r Rational) Sub(o Rational) Rational {
	return NewRational(r.Num*o.Den-o.Num*r.Den, r.Den*o.Den)
}

func (r Rational) Mul(o Rational) Rational {
	return NewRational(r.Num*o.Num, r.Den*o.Den)
}

// Div returns r / o.
// Panics if o is zero.
func (r Rational) Div(o Rational) Rational {
	if o.IsZero() {
		panic("rational: division by zero")
	}
	return NewRational(r.Num*o.Den, r.Den*o.Num)
}

func (r Rational) IsZero() bool {
	return r.Num == 0
}

func (r Rational) IsNegative() bool {
	return r.Num < 0
}

// Cmp compares r and o.
// Returns -1 if r < o, 0 if r == o, 1 if r > o.
func (r Rational) Cmp(o Rational) int {
	lhs := r.Num * o.Den
	rhs := o.Num * r.Den
	switch {
	case lhs < rhs:
		return -1
	case lhs > rhs:
		return 1
	default:
		return 0
	}
}

// CeilInt returns ceil(r) as int64.
// Go integer division truncates toward zero, so for negative fractions
// truncation already equals ceiling (e.g. -3/2 truncates to -1 = ceil(-1.5)).
// The remainder check only bumps up for positive non-integer values.
func (r Rational) CeilInt() int64 {
	q := r.Num / r.Den
	if r.Num%r.Den != 0 && r.Num > 0 {
		q++
	}
	return q
}

func (r Rational) String() string {
	if r.Den == 1 {
		return fmt.Sprintf("%d", r.Num)
	}
	return fmt.Sprintf("%d/%d", r.Num, r.Den)
}

// gcd returns the greatest common divisor of a and b (Euclidean algorithm).
func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// LCM returns the least common multiple of a slice of integers.
// Division before multiplication avoids intermediate overflow:
//
//	result = (result / gcd) * n   — not   result * n / gcd
//
// Returns 1 for an empty slice.
func LCM(numbers []int64) int64 {
	if len(numbers) == 0 {
		return 1
	}
	result := numbers[0]
	for _, n := range numbers[1:] {
		result = result / gcd(result, n) * n
	}
	return result
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
