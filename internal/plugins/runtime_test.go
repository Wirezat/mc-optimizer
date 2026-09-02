package plugins

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func loadFixture(t *testing.T, name string) *Program {
	t.Helper()
	src, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	prog, err := Compile("testmod", string(src))
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	return prog
}

func sampleContext() EvalContext {
	return EvalContext{
		Machine: EvalMachine{ModID: "testmod", MachineID: "mixer", Data: json.RawMessage(`{}`)},
		Recipe: EvalRecipe{
			ID:            "r1",
			DurationTicks: 20,
			Outputs:       []Output{{Ref: "testmod:dust", Amount: Rational{1, 1}, Probability: Rational{1, 1}}},
			Data:          json.RawMessage(`{}`),
		},
		Config: json.RawMessage(`{}`),
	}
}

func TestEvaluateReturnsVariant(t *testing.T) {
	prog := loadFixture(t, "trivial.js")
	got, err := prog.Evaluate(context.Background(), sampleContext())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 variant, got %d", len(got))
	}
	v := got[0]
	if v.ID != "base" {
		t.Errorf("ID = %q, want %q", v.ID, "base")
	}
	if v.Rate.Num != 1 || v.Rate.Den != 20 {
		t.Errorf("Rate = %d/%d, want 1/20", v.Rate.Num, v.Rate.Den)
	}
	if !v.Valid {
		t.Error("Valid = false, want true")
	}
	if len(v.Outputs) != 1 || v.Outputs[0].Ref != "testmod:dust" {
		t.Errorf("Outputs = %+v, want one testmod:dust", v.Outputs)
	}
}

func TestCompileRejectsMissingEvaluate(t *testing.T) {
	if _, err := Compile("testmod", "var plugin = { api_version: 1 }"); err == nil {
		t.Fatal("want error for plugin without evaluate, got nil")
	}
}

func TestCompileRejectsSyntaxError(t *testing.T) {
	if _, err := Compile("testmod", "var plugin = {"); err == nil {
		t.Fatal("want error for syntax error, got nil")
	}
}
