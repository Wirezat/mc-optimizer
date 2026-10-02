package importer

import (
	"strings"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
)

type recipeUnderTest struct {
	itemIn, itemOut   []resource.IO
	fluidIn, fluidOut []resource.IO
}

func byKind(ios []resource.IO, k resource.Kind) []resource.IO {
	var out []resource.IO
	for _, io := range ios {
		if io.Ref.Kind.Or() == k {
			out = append(out, io)
		}
	}
	return out
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
	return &recipeUnderTest{byKind(r.Inputs, resource.KindItem), byKind(r.Outputs, resource.KindItem), byKind(r.Inputs, resource.KindFluid), byKind(r.Outputs, resource.KindFluid)}
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
	want := [][2]int64{{3, 2}, {1, 2}, {1, 3}, {1, 2}, {1, 1}}
	for i, w := range want {
		in := got.itemIn[i]
		if in.Amount.Num != w[0] || in.Amount.Den != w[1] {
			t.Errorf("input %d amount = %s, want %d/%d", i, in.Amount, w[0], w[1])
		}
	}
	if out := got.itemOut[0]; out.Prob.Num != 33 || out.Prob.Den != 50 {
		t.Errorf("output 0 probability = %d/%d, want 33/50", out.Prob.Num, out.Prob.Den)
	}
	if out := got.itemOut[1]; out.Prob.Num != 1 || out.Prob.Den != 3 {
		t.Errorf("output 1 probability = %d/%d, want 1/3", out.Prob.Num, out.Prob.Den)
	}
	if out := got.itemOut[0]; out.Amount.Num != 1 || out.Amount.Den != 1 {
		t.Errorf("output amount = %d/%d, want the default 1/1", out.Amount.Num, out.Amount.Den)
	}
}

func TestParseModFile_ConsumedFalseIsATool(t *testing.T) {
	got := parseOneRecipe(t, `
  - machine: m
    duration_ticks: 10
    inputs:
      items:
        - item: tool
          consumed: false
        - item: ore
      fluids:
        - fluid: catalyst
          amount_mb: 10
          consumed: false
    outputs:
      items:
        - item: e
`)
	one := resource.NewRational(1, 1)
	if got.itemIn[0].Consumed || !got.itemIn[0].Prob.Eq(one) {
		t.Errorf("tool = %+v, want consumed false with probability 1", got.itemIn[0])
	}
	if !got.itemIn[1].Consumed {
		t.Errorf("ore = %+v, want consumed by default", got.itemIn[1])
	}
	if got.fluidIn[0].Consumed || !got.fluidIn[0].Prob.Eq(one) {
		t.Errorf("fluid tool = %+v, want consumed false with probability 1", got.fluidIn[0])
	}
}

func TestParseModFile_ToolErrorsNameWhere(t *testing.T) {
	_, err := ParseModFile([]byte(quantityHead + "  - machine: m\n    duration_ticks: 1\n    inputs:\n      items:\n        - item: hammer\n          probability: 0\n"))
	if err == nil || !strings.Contains(err.Error(), "line 9") || !strings.Contains(err.Error(), "consumed: false") {
		t.Errorf("error = %v, want line 9 and a hint at consumed: false", err)
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
		"input probability 0":   "    duration_ticks: 1\n    inputs:\n      items:\n        - item: a\n          probability: 0\n",
		"fluid input prob 0":    "    duration_ticks: 1\n    inputs:\n      fluids:\n        - fluid: water\n          amount_mb: 5\n          probability: 0\n",
		"consumed on output":    "    duration_ticks: 1\n    outputs:\n      items:\n        - item: a\n          consumed: false\n",
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

func TestParseModFile_TagKind(t *testing.T) {
	def, err := ParseModFile([]byte("mod_id: qmod\ntags:\n  - name: c:ingots\n    members: [qmod:a]\n  - name: c:honey\n    kind: fluid\n    members: [qmod:honey]\n"))
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if def.Tags[0].Kind != model.TagKindItem || def.Tags[1].Kind != model.TagKindFluid {
		t.Errorf("kinds = %q, %q; want item, fluid", def.Tags[0].Kind, def.Tags[1].Kind)
	}
	_, err = ParseModFile([]byte("mod_id: qmod\ntags:\n  - name: c:x\n    kind: gas\n"))
	if err == nil || !strings.Contains(err.Error(), "line 3") || !strings.Contains(err.Error(), "c:x") {
		t.Errorf("error = %v, want it to name line 3 and tag c:x", err)
	}
}
