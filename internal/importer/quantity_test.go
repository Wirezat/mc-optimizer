package importer

import (
	"strings"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
)

type recipeUnderTest struct {
	itemIn, itemOut   []model.ModIODef
	fluidIn, fluidOut []model.ModFluidIODef
}

const quantityHead = "mod_id: qmod\nmachines:\n  - id: m\nrecipes:\n"

func parseOneRecipe(t *testing.T, recipe string) *recipeUnderTest {
	t.Helper()
	def, err := ParseModFile([]byte(quantityHead + recipe))
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if len(def.Recipes) != 1 {
		t.Fatalf("recipes = %d, want 1", len(def.Recipes))
	}
	r := def.Recipes[0]
	return &recipeUnderTest{r.ItemInputs, r.ItemOutputs, r.FluidInputs, r.FluidOutputs}
}

func TestParseModFile_ExactAmounts(t *testing.T) {
	got := parseOneRecipe(t, `
  - machine: m
    duration_ticks: 10
    inputs:
      items:
        - item: a
          amount: 1.5
        - item: b
          amount: 0.5
        - item: c
          amount: "1/3"
        - item: d
          amount_num: 2
          amount_den: 4
        - item: f
    outputs:
      items:
        - item: e
          probability: 0.66
        - item: g
          probability: "1/3"
`)
	want := [][2]int{{3, 2}, {1, 2}, {1, 3}, {2, 4}, {1, 1}}
	for i, w := range want {
		in := got.itemIn[i]
		if in.AmountNum != w[0] || in.AmountDen != w[1] {
			t.Errorf("input %d amount = %d/%d, want %d/%d", i, in.AmountNum, in.AmountDen, w[0], w[1])
		}
	}
	if out := got.itemOut[0]; out.ProbNum != 33 || out.ProbDen != 50 {
		t.Errorf("output 0 probability = %d/%d, want 33/50", out.ProbNum, out.ProbDen)
	}
	if out := got.itemOut[1]; out.ProbNum != 1 || out.ProbDen != 3 {
		t.Errorf("output 1 probability = %d/%d, want 1/3", out.ProbNum, out.ProbDen)
	}
	if out := got.itemOut[0]; out.AmountNum != 1 || out.AmountDen != 1 {
		t.Errorf("output amount = %d/%d, want the default 1/1", out.AmountNum, out.AmountDen)
	}
}

func TestParseModFile_InputProbabilityZeroIsATool(t *testing.T) {
	got := parseOneRecipe(t, `
  - machine: m
    duration_ticks: 10
    inputs:
      items:
        - item: tool
          probability: 0.0
      fluids:
        - fluid: catalyst
          amount_mb: 10
          probability: 0
    outputs:
      items:
        - item: e
`)
	if got.itemIn[0].ProbNum != 0 || got.itemIn[0].ProbDen != 1 {
		t.Errorf("item input probability = %d/%d, want 0/1", got.itemIn[0].ProbNum, got.itemIn[0].ProbDen)
	}
	if got.fluidIn[0].ProbNum != 0 || got.fluidIn[0].ProbDen != 1 {
		t.Errorf("fluid input probability = %d/%d, want 0/1", got.fluidIn[0].ProbNum, got.fluidIn[0].ProbDen)
	}
}

func TestParseModFile_RejectsBadQuantities(t *testing.T) {
	for name, body := range map[string]string{
		"zero amount":           "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount: 0\n",
		"negative amount":       "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount: -1\n",
		"not a number":          "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount: lots\n",
		"num without den":       "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount_num: 2\n",
		"den without num":       "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount_den: 2\n",
		"amount and num":        "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount: 1\n          amount_num: 1\n          amount_den: 1\n",
		"zero den":              "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount_num: 1\n          amount_den: 0\n",
		"negative den":          "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount_num: 1\n          amount_den: -2\n",
		"probability above one": "    duration_ticks: 1\n    outputs:\n      items:\n        - item: a\n          probability: 1.5\n",
		"negative probability":  "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          probability: -0.1\n",
		"output probability 0":  "    duration_ticks: 1\n    outputs:\n      items:\n        - item: a\n          probability: 0\n",
		"fluid without amount":  "    duration_ticks: 1\n    inputs:\n      fluids:\n        - fluid: water\n",
		"fluid zero amount":     "    duration_ticks: 1\n    inputs:\n      fluids:\n        - fluid: water\n          amount_mb: 0\n",
		"fluid negative amount": "    duration_ticks: 1\n    outputs:\n      fluids:\n        - fluid: water\n          amount_mb: -5\n",
		"fluid output prob 0":   "    duration_ticks: 1\n    outputs:\n      fluids:\n        - fluid: water\n          amount_mb: 5\n          probability: 0\n",
		"fluid probability 2":   "    duration_ticks: 1\n    inputs:\n      fluids:\n        - fluid: water\n          amount_mb: 5\n          probability: 2\n",
		"zero duration":         "    duration_ticks: 0\n    outputs:\n      items:\n        - item: a\n",
		"missing duration":      "    outputs:\n      items:\n        - item: a\n",
		"amount out of range":   "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount: 99999999999\n",
	} {
		if _, err := ParseModFile([]byte(quantityHead + "  - machine: m\n" + body)); err == nil {
			t.Errorf("%s: ParseModFile succeeded, want an error", name)
		}
	}
}

func TestParseModFile_ErrorNamesWhere(t *testing.T) {
	src := quantityHead + `  - machine: m
    duration_ticks: 1
    outputs:
      items:
        - item: fine
  - machine: m
    duration_ticks: 1
    inputs:
      items:
        - item: ok
        - item: qmod:broken
          amount: 0
`
	_, err := ParseModFile([]byte(src))
	if err == nil {
		t.Fatal("ParseModFile succeeded, want an error")
	}
	for _, want := range []string{"line 15", "recipe on m", "input qmod:broken"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestParseModFile_DurationErrorNamesLine(t *testing.T) {
	_, err := ParseModFile([]byte(quantityHead + "  - machine: m\n    duration_ticks: 0\n"))
	if err == nil || !strings.Contains(err.Error(), "line 5") || !strings.Contains(err.Error(), "recipe on m") {
		t.Errorf("error = %v, want it to name line 5 and the recipe", err)
	}
}

func TestParseModFile_NumberErrorNamesLine(t *testing.T) {
	_, err := ParseModFile([]byte(quantityHead + "  - machine: m\n    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          amount: lots\n"))
	if err == nil || !strings.Contains(err.Error(), "line 10") {
		t.Errorf("error = %v, want it to name line 10", err)
	}
}
