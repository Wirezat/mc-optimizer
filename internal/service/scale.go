package service

import (
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// ErrBuiltCountExceeded reports a scale that would leave a group with fewer machines than
// the world already has standing.
var ErrBuiltCountExceeded = errors.New("scale would drop a group below its built machine count")

// scalePLRows computes what a saved line looks like at k times its size.
func scalePLRows(rateNum, rateDen int, groups []*model.MachineGroup, ios []*model.PLIO, k resource.Rational) (int, int, []db.ScaledGroup, []db.ScaledIO, error) {
	rate := resource.NewRational(int64(rateNum), int64(rateDen)).Mul(k)
	newRateNum, newRateDen, err := rateInts(rate)
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("service: scale line rate: %w", err)
	}

	scaledGroups := make([]db.ScaledGroup, 0, len(groups))
	for _, g := range groups {
		exact, count, _ := solver.ScaleCount(resource.NewRational(g.ExactCountNum, g.ExactCountDen), k)
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
		num, den, err := rateInts(resource.NewRational(int64(io.RateNum), int64(io.RateDen)).Mul(k))
		if err != nil {
			return 0, 0, nil, nil, fmt.Errorf("service: scale %s rate: %w", io.ItemFluidID, err)
		}
		scaledIOs = append(scaledIOs, db.ScaledIO{ID: io.ID, RateNum: num, RateDen: den})
	}
	return newRateNum, newRateDen, scaledGroups, scaledIOs, nil
}

// rateInts narrows a rate to the int columns the rate rows are stored in.
func rateInts(r resource.Rational) (int, int, error) {
	const maxInt32 = 1<<31 - 1
	if r.Num > maxInt32 || r.Num < -maxInt32 || r.Den > maxInt32 {
		return 0, 0, fmt.Errorf("rate %v does not fit the stored rate columns", r)
	}
	return int(r.Num), int(r.Den), nil
}
