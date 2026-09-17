package solver

import "testing"

// Scaling a line by a whole number is what turns a part-loaded group into a
// full one: 1½ machines' worth of work needs 2 machines at 75%, twice that
// needs exactly 3 at 100%.
func TestScaleCount_WholeFactorFillsAPartialGroup(t *testing.T) {
	exact, count, util := ScaleCount(NewRational(3, 2), RationalFromInt(2))

	if want := NewRational(3, 1); !exact.Eq(want) {
		t.Errorf("exact = %v, want %v", exact, want)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if want := NewRational(1, 1); !util.Eq(want) {
		t.Errorf("utilization = %v, want %v", util, want)
	}
}

// A fully loaded group stays fully loaded — the point of scaling by a whole
// number is that it preserves the base solution's utilisation.
func TestScaleCount_WholeFactorKeepsFullGroupsFull(t *testing.T) {
	_, count, util := ScaleCount(RationalFromInt(4), RationalFromInt(3))

	if count != 12 {
		t.Errorf("count = %d, want 12", count)
	}
	if want := NewRational(1, 1); !util.Eq(want) {
		t.Errorf("utilization = %v, want %v", util, want)
	}
}

// A fractional factor is allowed and may leave idle capacity, the same way a
// partial group does today: 2 × 1.5 = 3 exact, 4 × 1.5 = 6 exact, but
// 1 × 1.5 needs a second machine that only runs three quarters of the time.
func TestScaleCount_FractionalFactorLeavesIdleCapacity(t *testing.T) {
	exact, count, util := ScaleCount(RationalFromInt(1), NewRational(3, 2))

	if want := NewRational(3, 2); !exact.Eq(want) {
		t.Errorf("exact = %v, want %v", exact, want)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if want := NewRational(3, 4); !util.Eq(want) {
		t.Errorf("utilization = %v, want %v", util, want)
	}
}

// Scaling down is scaling too, but a group never drops below one machine —
// zero machines would silently delete the step from the line.
func TestScaleCount_NeverGoesBelowOneMachine(t *testing.T) {
	_, count, _ := ScaleCount(RationalFromInt(1), NewRational(1, 4))

	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}
