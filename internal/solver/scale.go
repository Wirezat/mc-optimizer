package solver

import "github.com/Wirezat/production-optimizer/internal/resource"

// ScaleCount scales an exact machine count by k: new exact count, whole machines, utilisation.
func ScaleCount(exact, k resource.Rational) (scaled resource.Rational, count int64, utilization resource.Rational) {
	scaled = exact.Mul(k)
	count = max(scaled.CeilInt(), 1)
	return scaled, count, scaled.Div(resource.RationalFromInt(count))
}

// ScaleResult returns result multiplied by k.
func ScaleResult(result SolveResult, k resource.Rational) SolveResult {
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
