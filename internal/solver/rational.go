package solver

import "fmt"

type Rational struct{ Num, Den int64 }

func norm(n, d int64) Rational {
	if d < 0 {
		n, d = -n, -d
	}
	a := n
	if a < 0 {
		a = -a
	}
	if g := gcd(a, d); g > 1 {
		n, d = n/g, d/g
	}
	return Rational{n, d}
}

func NewRational(n, d int64) Rational {
	if d == 0 {
		panic("rational: denominator must not be zero")
	}
	return norm(n, d)
}

func RationalFromInt(n int64) Rational { return Rational{n, 1} }

func (r Rational) Add(o Rational) Rational {
	if r.Num == 0 {
		return o
	}
	if o.Num == 0 {
		return r
	}
	if r.Den == o.Den {
		return norm(r.Num+o.Num, r.Den)
	}
	g := gcd(r.Den, o.Den)
	rd, od := r.Den/g, o.Den/g
	return norm(r.Num*od+o.Num*rd, r.Den*od)
}

func (r Rational) Sub(o Rational) Rational {
	if o.Num == 0 {
		return r
	}
	if r.Num == 0 {
		return Rational{-o.Num, o.Den}
	}
	if r.Den == o.Den {
		return norm(r.Num-o.Num, r.Den)
	}
	g := gcd(r.Den, o.Den)
	rd, od := r.Den/g, o.Den/g
	return norm(r.Num*od-o.Num*rd, r.Den*od)
}

func (r Rational) Mul(o Rational) Rational {
	if r.Num == 0 || o.Num == 0 {
		return Rational{0, 1}
	}
	rn, on := r.Num, o.Num
	if rn < 0 {
		rn = -rn
	}
	if on < 0 {
		on = -on
	}
	g1, g2 := gcd(rn, o.Den), gcd(on, r.Den)
	n := (r.Num / g1) * (o.Num / g2)
	d := (r.Den / g2) * (o.Den / g1)
	if d < 0 {
		n, d = -n, -d
	}
	return Rational{n, d}
}

func (r Rational) Div(o Rational) Rational {
	if o.Num == 0 {
		panic("rational: division by zero")
	}
	rec := Rational{o.Den, o.Num}
	if rec.Den < 0 {
		rec.Num, rec.Den = -rec.Num, -rec.Den
	}
	return r.Mul(rec)
}

func (r Rational) IsZero() bool     { return r.Num == 0 }
func (r Rational) IsNegative() bool { return r.Num < 0 }

// Cmp: overflow-sichere Variante via Kreuzsubtraktion
func (r Rational) Cmp(o Rational) int {
	if r.Num == o.Num && r.Den == o.Den {
		return 0
	}
	// (r.Num/r.Den) - (o.Num/o.Den) = (r.Num*o.Den - o.Num*r.Den) / (r.Den*o.Den)
	// Da Den immer > 0, reicht Vorzeichen des Zählers.
	d := r.Num*o.Den - o.Num*r.Den
	if d < 0 {
		return -1
	}
	if d > 0 {
		return 1
	}
	return 0
}

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

// Euklidischer GCD – schneller als Stein auf moderner Hardware.
func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func LCM(ns []int64) int64 {
	if len(ns) == 0 {
		return 1
	}
	r := ns[0]
	if r < 0 {
		r = -r
	}
	for _, n := range ns[1:] {
		if n < 0 {
			n = -n
		}
		r = r / gcd(r, n) * n
	}
	return r
}
