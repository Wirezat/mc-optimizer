package solver

import (
	"fmt"
	"math"
)

type Rational struct{ Num, Den int64 }

func abs64u(n int64) uint64 {
	if n >= 0 {
		return uint64(n)
	}
	return ^uint64(n) + 1
}

func ugcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func gcd(a, b int64) int64 { return int64(ugcd(abs64u(a), abs64u(b))) }

// mulHiLo computes the full 128-bit product of two uint64s.
func mulHiLo(a, b uint64) (hi, lo uint64) {
	a0, a1 := a&0xFFFFFFFF, a>>32
	b0, b1 := b&0xFFFFFFFF, b>>32
	w0 := a0 * b0
	t := a1*b0 + (w0 >> 32)
	w1 := (t & 0xFFFFFFFF) + a0*b1
	return a1*b1 + (t >> 32) + (w1 >> 32), a * b
}

func smul(a, b int64) int64 {
	hi, lo := mulHiLo(abs64u(a), abs64u(b))
	if hi != 0 || lo > math.MaxInt64 {
		panic("rational: int64 overflow")
	}
	if (a < 0) != (b < 0) {
		return -int64(lo)
	}
	return int64(lo)
}

func sadd(a, b int64) int64 {
	s := a + b
	if (a^b) >= 0 && (s^a) < 0 {
		panic("rational: int64 overflow")
	}
	return s
}

// cmpCross compares a/b vs c/d via cross-multiplication, avoiding overflow.
func cmpCross(a, b, c, d int64) int {
	if sa, sc := a >= 0, c >= 0; sa != sc {
		if sa {
			return 1
		}
		return -1
	}
	lhi, llo := mulHiLo(abs64u(a), uint64(b))
	rhi, rlo := mulHiLo(abs64u(c), uint64(d))
	var lgt bool
	switch {
	case lhi != rhi:
		lgt = lhi > rhi
	case llo != rlo:
		lgt = llo > rlo
	default:
		return 0
	}
	if a >= 0 {
		if lgt {
			return 1
		}
		return -1
	}
	if lgt {
		return -1
	}
	return 1
}

func norm(n, d int64) Rational {
	if d < 0 {
		n, d = -n, -d
	}
	if g := gcd(n, d); g > 1 {
		n, d = n/g, d/g
	}
	return Rational{n, d}
}

// normPos assumes d > 0 (avoids redundant sign check).
func normPos(n, d int64) Rational {
	if g := gcd(n, d); g > 1 {
		n, d = n/g, d/g
	}
	return Rational{n, d}
}

func NewRational(n, d int64) Rational {
	if d == 0 {
		panic("rational: zero denominator")
	}
	if n == math.MinInt64 || d == math.MinInt64 {
		panic("rational: MinInt64 unsupported")
	}
	return norm(n, d)
}

func RationalFromInt(n int64) Rational {
	if n == math.MinInt64 {
		panic("rational: MinInt64 unsupported")
	}
	return Rational{n, 1}
}

func (r Rational) Neg() Rational { return Rational{-r.Num, r.Den} }

func (r Rational) Abs() Rational {
	if r.Num < 0 {
		return Rational{-r.Num, r.Den}
	}
	return r
}

func (r Rational) Add(o Rational) Rational {
	switch {
	case r.Num == 0:
		return o
	case o.Num == 0:
		return r
	case r.Den == o.Den:
		return normPos(sadd(r.Num, o.Num), r.Den)
	}
	g := gcd(r.Den, o.Den)
	rd, od := r.Den/g, o.Den/g
	return normPos(sadd(smul(r.Num, od), smul(o.Num, rd)), smul(r.Den, od))
}

func (r Rational) Sub(o Rational) Rational {
	switch {
	case o.Num == 0:
		return r
	case r.Num == 0:
		return o.Neg()
	case r.Den == o.Den:
		return normPos(sadd(r.Num, -o.Num), r.Den)
	}
	g := gcd(r.Den, o.Den)
	rd, od := r.Den/g, o.Den/g
	return normPos(sadd(smul(r.Num, od), smul(-o.Num, rd)), smul(r.Den, od))
}

func (r Rational) Mul(o Rational) Rational {
	if r.Num == 0 || o.Num == 0 {
		return Rational{0, 1}
	}
	g1, g2 := gcd(r.Num, o.Den), gcd(o.Num, r.Den)
	return Rational{smul(r.Num/g1, o.Num/g2), smul(r.Den/g2, o.Den/g1)}
}

func (r Rational) Div(o Rational) Rational {
	if o.Num == 0 {
		panic("rational: division by zero")
	}
	return r.Mul(norm(o.Den, o.Num))
}

func (r Rational) IsZero() bool       { return r.Num == 0 }
func (r Rational) IsNegative() bool   { return r.Num < 0 }
func (r Rational) IsPositive() bool   { return r.Num > 0 }
func (r Rational) Eq(o Rational) bool { return r.Num == o.Num && r.Den == o.Den }

func (r Rational) Cmp(o Rational) int {
	return cmpCross(r.Num, o.Den, o.Num, r.Den)
}

func (r Rational) CeilInt() int64 {
	q := r.Num / r.Den
	if r.Num%r.Den != 0 && r.Num > 0 {
		q++
	}
	return q
}

func (r Rational) FloorInt() int64 {
	q := r.Num / r.Den
	if r.Num%r.Den != 0 && r.Num < 0 {
		q--
	}
	return q
}

func (r Rational) String() string {
	if r.Den == 1 {
		return fmt.Sprintf("%d", r.Num)
	}
	return fmt.Sprintf("%d/%d", r.Num, r.Den)
}

func LCM(ns []int64) int64 {
	if len(ns) == 0 {
		return 1
	}
	if ns[0] == math.MinInt64 {
		panic("LCM: MinInt64 unsupported")
	}
	r := int64(abs64u(ns[0]))
	for _, n := range ns[1:] {
		if n == math.MinInt64 {
			panic("LCM: MinInt64 unsupported")
		}
		n = int64(abs64u(n))
		if n == 0 || r == 0 {
			return 0
		}
		r = smul(r/gcd(r, n), n)
	}
	return r
}
