package solver

import (
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// Scaling by a whole number turns a part-loaded group into a full one.
func TestScaleCount_WholeFactorFillsAPartialGroup(t *testing.T) {
	exact, count, util := ScaleCount(resource.NewRational(3, 2), resource.RationalFromInt(2))

	if want := resource.NewRational(3, 1); !exact.Eq(want) {
		t.Errorf("exact = %v, want %v", exact, want)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if want := resource.NewRational(1, 1); !util.Eq(want) {
		t.Errorf("utilization = %v, want %v", util, want)
	}
}

// A fully loaded group stays fully loaded.
func TestScaleCount_WholeFactorKeepsFullGroupsFull(t *testing.T) {
	_, count, util := ScaleCount(resource.RationalFromInt(4), resource.RationalFromInt(3))

	if count != 12 {
		t.Errorf("count = %d, want 12", count)
	}
	if want := resource.NewRational(1, 1); !util.Eq(want) {
		t.Errorf("utilization = %v, want %v", util, want)
	}
}

// A fractional factor is allowed and may leave idle capacity.
func TestScaleCount_FractionalFactorLeavesIdleCapacity(t *testing.T) {
	exact, count, util := ScaleCount(resource.RationalFromInt(1), resource.NewRational(3, 2))

	if want := resource.NewRational(3, 2); !exact.Eq(want) {
		t.Errorf("exact = %v, want %v", exact, want)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if want := resource.NewRational(3, 4); !util.Eq(want) {
		t.Errorf("utilization = %v, want %v", util, want)
	}
}

// Scaling down never drops a group below one machine.
func TestScaleCount_NeverGoesBelowOneMachine(t *testing.T) {
	_, count, _ := ScaleCount(resource.RationalFromInt(1), resource.NewRational(1, 4))

	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

// Scaling a solved line multiplies what it produces and consumes, and lets a part-loaded group fill up.
func TestScaleResult(t *testing.T) {
	item := resource.Ref{ModID: "minecraft", ID: "copper_ingot"}
	result := SolveResult{
		MachineGroups: []MachineGroupDraft{
			{MachineID: "macerator", Count: 2, ExactCount: resource.NewRational(3, 2)},
			{MachineID: "furnace", Count: 1, ExactCount: resource.RationalFromInt(1)},
		},
		IOProfile: IOProfile{
			Inputs:  []IOEntry{{Item: item, Rate: resource.NewRational(1, 2)}},
			Outputs: []IOEntry{{Item: item, Rate: resource.RationalFromInt(4)}},
		},
		ActualRate: resource.RationalFromInt(4),
	}

	scaled := ScaleResult(result, resource.RationalFromInt(2))

	if got := scaled.MachineGroups[0]; got.Count != 3 || !got.Utilization.Eq(resource.RationalFromInt(1)) {
		t.Errorf("partial group scaled to count=%d util=%v, want 3 at 100%%", got.Count, got.Utilization)
	}
	if got := scaled.MachineGroups[1].Count; got != 2 {
		t.Errorf("full group count = %d, want 2", got)
	}
	if got := scaled.IOProfile.Inputs[0].Rate; !got.Eq(resource.RationalFromInt(1)) {
		t.Errorf("input rate = %v, want 1", got)
	}
	if got := scaled.IOProfile.Outputs[0].Rate; !got.Eq(resource.RationalFromInt(8)) {
		t.Errorf("output rate = %v, want 8", got)
	}
	if got := scaled.ActualRate; !got.Eq(resource.RationalFromInt(8)) {
		t.Errorf("actual rate = %v, want 8", got)
	}
	if result.MachineGroups[0].Count != 2 {
		t.Error("input result was mutated")
	}
}
