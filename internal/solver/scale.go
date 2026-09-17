package solver

// ScaleCount scales one machine group's exact count by k and returns the new
// exact count together with the whole machines that need building and the
// utilisation that follows. A whole-number k keeps a fully loaded group fully
// loaded and fills a part-loaded one; a fractional k may leave idle capacity,
// like a partial group does.
func ScaleCount(exact, k Rational) (scaled Rational, count int64, utilization Rational) {
	scaled = exact.Mul(k)
	count = max(scaled.CeilInt(), 1)
	return scaled, count, scaled.Div(RationalFromInt(count))
}
