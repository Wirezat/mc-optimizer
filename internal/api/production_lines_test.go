package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/solver"
)

// A too-large factory surfaces as ErrRateOverflow/ErrRateDomain from the rate arithmetic;
// the client needs a structured code (not a raw Go error string) so it can show translated
// text instead of an opaque 500.
func TestWriteSolveErrorRateOverflow(t *testing.T) {
	for _, err := range []error{solver.ErrRateOverflow, solver.ErrRateDomain} {
		w := httptest.NewRecorder()
		if !writeSolveError(w, err) {
			t.Fatalf("writeSolveError(%v) = false, want true", err)
		}
		if w.Code != 422 {
			t.Errorf("status = %d, want 422", w.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal body %q: %v", w.Body.String(), err)
		}
		if body["error"] != "RATE_OVERFLOW" {
			t.Errorf(`body["error"] = %q, want "RATE_OVERFLOW"`, body["error"])
		}
	}
}

// A chain that only balances by running a recipe backwards is a catalog condition, not a
// server fault: 422 with a structured code, never a 500 whose raw Go text would reach the
// unauthenticated demo endpoint.
func TestWriteSolveErrorNegativeRate(t *testing.T) {
	inner := &solver.ErrNegativeRate{RecipeID: "mi:cutting_machine/steel"}
	for name, err := range map[string]error{
		"bare":    error(inner),
		"wrapped": fmt.Errorf("solver: linear system solve: %w", inner),
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			if !writeSolveError(w, err) {
				t.Fatalf("writeSolveError(%v) = false, want true", err)
			}
			if w.Code != 422 {
				t.Errorf("status = %d, want 422", w.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal body %q: %v", w.Body.String(), err)
			}
			if body["error"] != "NEGATIVE_RATE" {
				t.Errorf(`body["error"] = %q, want "NEGATIVE_RATE"`, body["error"])
			}
			if strings.Contains(w.Body.String(), inner.RecipeID) {
				t.Errorf("body %q leaks the recipe id", w.Body.String())
			}
		})
	}
}

func TestWriteSolveErrorNil(t *testing.T) {
	w := httptest.NewRecorder()
	if writeSolveError(w, nil) {
		t.Error("writeSolveError(nil) = true, want false")
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty (nothing written)", w.Body.String())
	}
}
