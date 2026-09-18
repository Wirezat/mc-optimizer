package api

import (
	"math"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

func TestAggregateCostsSumsPerResource(t *testing.T) {
	groups := [][]plugins.Cost{
		{{Resource: "eu", Amount: plugins.Rational{Num: 128, Den: 1}}},
		{{Resource: "eu", Amount: plugins.Rational{Num: 32, Den: 1}},
			{Resource: "steam", Amount: plugins.Rational{Num: 1, Den: 2}}},
	}
	got := aggregateCosts(groups)
	if len(got) != 2 {
		t.Fatalf("want 2 resources, got %d: %+v", len(got), got)
	}
	if eu := findCost(got, "eu"); eu.Num != 160 || eu.Den != 1 {
		t.Errorf("eu = %d/%d, want 160/1", eu.Num, eu.Den)
	}
	if steam := findCost(got, "steam"); steam.Num != 1 || steam.Den != 2 {
		t.Errorf("steam = %d/%d, want 1/2", steam.Num, steam.Den)
	}
}

func TestAggregateCostsIsStablyOrdered(t *testing.T) {
	groups := [][]plugins.Cost{{
		{Resource: "steam", Amount: plugins.Rational{Num: 1, Den: 1}},
		{Resource: "eu", Amount: plugins.Rational{Num: 1, Den: 1}},
	}}
	a := aggregateCosts(groups)
	b := aggregateCosts(groups)
	for i := range a {
		if a[i].Resource != b[i].Resource {
			t.Fatalf("order is not stable: %+v vs %+v", a, b)
		}
	}
	if a[0].Resource != "eu" {
		t.Errorf("first resource = %q, want alphabetical (eu)", a[0].Resource)
	}
}

func TestAggregateCostsDropsOverflowingResourceInsteadOfCrashing(t *testing.T) {
	groups := [][]plugins.Cost{
		{{Resource: "eu", Amount: plugins.Rational{Num: math.MaxInt64, Den: 1}}},
		{{Resource: "eu", Amount: plugins.Rational{Num: 1, Den: 1}}},
		{{Resource: "steam", Amount: plugins.Rational{Num: 5, Den: 1}}},
	}
	got := aggregateCosts(groups) // must not panic
	if eu := findCost(got, "eu"); eu != (plugins.Rational{}) {
		t.Errorf("overflowing resource eu should be dropped, got %+v", eu)
	}
	if steam := findCost(got, "steam"); steam.Num != 5 || steam.Den != 1 {
		t.Errorf("steam = %d/%d, want 5/1 (unaffected by eu's overflow)", steam.Num, steam.Den)
	}
}

func TestScaleCostsMultipliesAmountByCount(t *testing.T) {
	got := scaleCosts([]plugins.Cost{{Resource: "eu", Amount: plugins.Rational{Num: 32, Den: 1}}}, 4)
	if len(got) != 1 {
		t.Fatalf("want 1 cost, got %d: %+v", len(got), got)
	}
	if got[0].Amount.Num != 128 || got[0].Amount.Den != 1 {
		t.Errorf("amount = %d/%d, want 128/1", got[0].Amount.Num, got[0].Amount.Den)
	}
}

func TestScaleCostsWithNonPositiveCountYieldsNothing(t *testing.T) {
	got := scaleCosts([]plugins.Cost{{Resource: "eu", Amount: plugins.Rational{Num: 32, Den: 1}}}, 0)
	if len(got) != 0 {
		t.Errorf("want no costs for a zero count, got %+v", got)
	}
}

func findCost(cs []plugins.Cost, resource string) plugins.Rational {
	for _, c := range cs {
		if c.Resource == resource {
			return c.Amount
		}
	}
	return plugins.Rational{}
}
