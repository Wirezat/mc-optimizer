package solver

import (
	"testing"
)

func TestPartialMachineReduction(t *testing.T) {
	// Reflects real redstone_battery chain:
	//   assembler   ExactCount = 10   (ticks=200, rate=1/20)
	//   bending     ExactCount = 10   (ticks=50,  rate=1/5)
	//   compressor  ExactCount = 25/2 (ticks=100, rate=1/4, output=2)
	// Without partial: k=2, GCD=5 → 4/4/5 at 0.4/s
	assemblerID := "assembler-recipe"
	bendingID := "bending-recipe"
	compressorID := "compressor-recipe"

	// Named by RateKey: the ladder may swap the group's recipe for a sibling.
	key := func(recipeID, machineID string) string {
		return RecipeOptionKey(recipeID, "mi", machineID)
	}
	baseGroups := func() []MachineGroupDraft {
		return []MachineGroupDraft{
			{RecipeID: assemblerID, MachineID: "assembler", RateKey: key(assemblerID, "assembler"),
				Count: 1, ExactCount: NewRational(10, 1)},
			{RecipeID: bendingID, MachineID: "bending_machine", RateKey: key(bendingID, "bending_machine"),
				Count: 1, ExactCount: NewRational(10, 1)},
			{RecipeID: compressorID, MachineID: "compressor", RateKey: key(compressorID, "compressor"),
				Count: 1, ExactCount: NewRational(25, 2)},
		}
	}

	rv := RateVector{
		ItemRates:   map[string]Rational{"mi:redstone_battery": NewRational(1, 20)},
		RecipeRates: map[string]Rational{},
	}
	root := ItemRef{ModID: "mi", ItemID: "redstone_battery"}

	s := &Solver{AutoScaleMax: 500}

	t.Run("no_partial", func(t *testing.T) {
		groups, rate, k, _ := s.ScaleToInteger(baseGroups(), rv, root, 500, nil)
		if k != 2 {
			t.Errorf("k=%d want 2", k)
		}
		rateS := rate.Mul(NewRational(20, 1))
		if rateS.Num != 2 || rateS.Den != 5 {
			t.Errorf("rate=%v/%v want 0.4/s", rateS.Num, rateS.Den)
		}
		counts := [3]int64{groups[0].Count, groups[1].Count, groups[2].Count}
		if counts != [3]int64{4, 4, 5} {
			t.Errorf("counts=%v want [4 4 5]", counts)
		}
		t.Logf("=== NO PARTIAL === Rate: %v/%v/s  %dx assembler %v%%, %dx bending %v%%, %dx compressor %v%%",
			rateS.Num, rateS.Den, groups[0].Count, groups[0].Utilization, groups[1].Count, groups[1].Utilization, groups[2].Count, groups[2].Utilization)
	})

	t.Run("partial_on_compressor", func(t *testing.T) {
		ap := map[string]bool{key(compressorID, "compressor"): true}
		groups, rate, _, _ := s.ScaleToInteger(baseGroups(), rv, root, 500, ap)
		rateS := rate.Mul(NewRational(20, 1))
		t.Logf("=== PARTIAL on compressor === Rate: %v/%v/s  %dx assembler util=%v, %dx bending util=%v, %dx compressor util=%v",
			rateS.Num, rateS.Den, groups[0].Count, groups[0].Utilization, groups[1].Count, groups[1].Utilization, groups[2].Count, groups[2].Utilization)
		// Non-partial (assembler, bending): LCM(1,1)=1, GCD(10,10)=10 → each ExactCount=1 at 100%.
		// Partial compressor: ExactCount=1.25 → ceil=2 at 62.5%. Rate=0.1/s.
		if groups[0].Count != 1 {
			t.Errorf("assembler count=%d want 1", groups[0].Count)
		}
		if groups[1].Count != 1 {
			t.Errorf("bending count=%d want 1", groups[1].Count)
		}
		if groups[2].Count != 2 {
			t.Errorf("compressor count=%d want 2", groups[2].Count)
		}
		// assembler and bending at 100%
		if groups[0].Utilization.Num != groups[0].Utilization.Den {
			t.Errorf("assembler util=%v want 1", groups[0].Utilization)
		}
		if groups[1].Utilization.Num != groups[1].Utilization.Den {
			t.Errorf("bending util=%v want 1", groups[1].Utilization)
		}
		// compressor util = 5/4 / 2 = 5/8
		u := groups[2].Utilization
		if u.Num*8 != u.Den*5 {
			t.Errorf("compressor util=%v/%v want 5/8", u.Num, u.Den)
		}
		// rate = 0.1/s = 1/10
		if rateS.Num*10 != rateS.Den*1 {
			t.Errorf("rate=%v/%v want 0.1/s", rateS.Num, rateS.Den)
		}
	})

	t.Run("partial_on_bending", func(t *testing.T) {
		ap := map[string]bool{bendingID: true}
		groups, rate, _, _ := s.ScaleToInteger(baseGroups(), rv, root, 500, ap)
		rateS := rate.Mul(NewRational(20, 1))
		t.Logf("=== PARTIAL on bending === Rate: %v/%v/s  %dx assembler util=%v, %dx bending util=%v, %dx compressor util=%v",
			rateS.Num, rateS.Den, groups[0].Count, groups[0].Utilization, groups[1].Count, groups[1].Utilization, groups[2].Count, groups[2].Utilization)
		// Non-partial (assembler, compressor): LCM(1,2)=2, GCD(20,25)=5 → assembler=4, compressor=5.
		// Bending ExactCount after scaling = 4 (integer) → ceil=4 at 100%. Same as no-partial.
		// (bending:assembler is exactly 1:1 for this chain, so partial has no effect here.)
		if groups[0].Count != 4 {
			t.Errorf("assembler count=%d want 4", groups[0].Count)
		}
		if groups[1].Count != 4 {
			t.Errorf("bending count=%d want 4", groups[1].Count)
		}
		if groups[2].Count != 5 {
			t.Errorf("compressor count=%d want 5", groups[2].Count)
		}
		// rate = 0.4/s = 2/5
		if rateS.Num*5 != rateS.Den*2 {
			t.Errorf("rate=%v/%v want 0.4/s", rateS.Num, rateS.Den)
		}
	})
}
