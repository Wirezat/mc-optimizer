package solver

// ScaleCount scales one machine group's exact count by k and returns the new exact count
// together with the whole machines that need building and the utilisation that follows.
func ScaleCount(exact, k Rational) (scaled Rational, count int64, utilization Rational) {
	scaled = exact.Mul(k)
	count = max(scaled.CeilInt(), 1)
	return scaled, count, scaled.Div(RationalFromInt(count))
}

// ScaleResult returns result multiplied by k: machine counts follow from the scaled exact
// counts, every rate scales with it.
func ScaleResult(result SolveResult, k Rational) SolveResult {
	out := result
	out.MachineGroups = make([]MachineGroupDraft, len(result.MachineGroups))
	copy(out.MachineGroups, result.MachineGroups)
	for i := range out.MachineGroups {
		g := &out.MachineGroups[i]
		g.ExactCount, g.Count, g.Utilization = ScaleCount(g.ExactCount, k)
	}

	scaleEntries := func(src []IOEntry) []IOEntry {
		dst := make([]IOEntry, len(src))
		copy(dst, src)
		for i := range dst {
			dst[i].Rate = dst[i].Rate.Mul(k)
		}
		return dst
	}
	out.IOProfile.Inputs = scaleEntries(result.IOProfile.Inputs)
	out.IOProfile.Outputs = scaleEntries(result.IOProfile.Outputs)
	out.ActualRate = result.ActualRate.Mul(k)
	return out
}
