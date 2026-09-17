package service

import (
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// ErrBuiltCountExceeded reports a scale that would leave a group with fewer
// machines than the world already has standing.
var ErrBuiltCountExceeded = errors.New("scale would drop a group below its built machine count")

// scaleSolveResult returns result multiplied by k: machine counts follow from
// the scaled exact counts, every rate scales with it. The input is left alone.
func scaleSolveResult(result solver.SolveResult, k solver.Rational) solver.SolveResult {
	out := result
	out.MachineGroups = make([]solver.MachineGroupDraft, len(result.MachineGroups))
	copy(out.MachineGroups, result.MachineGroups)
	for i := range out.MachineGroups {
		g := &out.MachineGroups[i]
		exact, count, util := solver.ScaleCount(g.ExactCount, k)
		g.ExactCount, g.Count, g.Utilization = exact, count, util
	}

	scaleEntries := func(src []solver.IOEntry) []solver.IOEntry {
		dst := make([]solver.IOEntry, len(src))
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

// scalePLRows computes what a saved line looks like at k times its size. Rows
// keep their identity so build state survives; a group that would end up with
// fewer machines than are already built fails the whole scale.
func scalePLRows(rateNum, rateDen int, groups []*model.MachineGroup, ios []*model.PLIO, k solver.Rational) (int, int, []db.ScaledGroup, []db.ScaledIO, error) {
	rate := solver.NewRational(int64(rateNum), int64(rateDen)).Mul(k)
	newRateNum, newRateDen, err := rateInts(rate)
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("service: scale line rate: %w", err)
	}

	scaledGroups := make([]db.ScaledGroup, 0, len(groups))
	for _, g := range groups {
		exact, count, _ := solver.ScaleCount(solver.NewRational(g.ExactCountNum, g.ExactCountDen), k)
		if int(count) < g.BuiltCount {
			return 0, 0, nil, nil, fmt.Errorf("%w: %s would hold %d machines with %d built",
				ErrBuiltCountExceeded, g.MachineID, count, g.BuiltCount)
		}
		scaledGroups = append(scaledGroups, db.ScaledGroup{
			ID: g.ID, Count: int(count), ExactNum: exact.Num, ExactDen: exact.Den,
		})
	}

	scaledIOs := make([]db.ScaledIO, 0, len(ios))
	for _, io := range ios {
		num, den, err := rateInts(solver.NewRational(int64(io.RateNum), int64(io.RateDen)).Mul(k))
		if err != nil {
			return 0, 0, nil, nil, fmt.Errorf("service: scale %s rate: %w", io.ItemFluidID, err)
		}
		scaledIOs = append(scaledIOs, db.ScaledIO{ID: io.ID, RateNum: num, RateDen: den})
	}
	return newRateNum, newRateDen, scaledGroups, scaledIOs, nil
}

// rateInts narrows a rate to the int columns the rate rows are stored in.
func rateInts(r solver.Rational) (int, int, error) {
	const maxInt32 = 1<<31 - 1
	if r.Num > maxInt32 || r.Num < -maxInt32 || r.Den > maxInt32 {
		return 0, 0, fmt.Errorf("rate %v does not fit the stored rate columns", r)
	}
	return int(r.Num), int(r.Den), nil
}
