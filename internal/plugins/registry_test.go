package plugins

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func trivialSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("testdata/trivial.js")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(src)
}

func TestRegistryReusesCompiledProgram(t *testing.T) {
	r := NewRegistry()
	if err := r.Put("testmod", "1.0.0", trivialSource(t)); err != nil {
		t.Fatalf("put: %v", err)
	}
	a, ok := r.Get("testmod", "1.0.0")
	if !ok {
		t.Fatal("Get after Put returned ok=false")
	}
	b, _ := r.Get("testmod", "1.0.0")
	if a != b {
		t.Error("Get returned two different programs for the same version")
	}
}

func TestRegistryMissesOnVersionChange(t *testing.T) {
	r := NewRegistry()
	if err := r.Put("testmod", "1.0.0", trivialSource(t)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, ok := r.Get("testmod", "2.0.0"); ok {
		t.Error("Get with a different version must miss")
	}
}

func TestRegistryDrop(t *testing.T) {
	r := NewRegistry()
	if err := r.Put("testmod", "1.0.0", trivialSource(t)); err != nil {
		t.Fatalf("put: %v", err)
	}
	r.Drop("testmod")
	if _, ok := r.Get("testmod", "1.0.0"); ok {
		t.Error("Get after Drop must miss")
	}
}

func TestRegistryPutRejectsBadSource(t *testing.T) {
	r := NewRegistry()
	if err := r.Put("testmod", "1.0.0", "var plugin = {"); err == nil {
		t.Fatal("want error for unparseable source, got nil")
	}
}

func TestRegistryConcurrentEvaluate(t *testing.T) {
	r := NewRegistry()
	if err := r.Put("testmod", "1.0.0", trivialSource(t)); err != nil {
		t.Fatalf("put: %v", err)
	}
	prog, _ := r.Get("testmod", "1.0.0")

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := prog.Evaluate(context.Background(), sampleContext()); err != nil {
				t.Errorf("concurrent evaluate: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestRegistryConcurrentPutGet asserts the registry's own invariant: Put and Get are
// callable concurrently on the same modID without a data race, and a caller that gets a hit
// never observes a torn entry - a returned program compiled under a different version than
// the one it reports matching.
func TestRegistryConcurrentPutGet(t *testing.T) {
	srcV1 := `var plugin = {
		evaluate: function (ctx) {
			return [{ id: "v100", label: "", rate: { num: 1, den: 1 }, costs: [], outputs: [], items: [], valid: true }];
		}
	}`
	srcV2 := `var plugin = {
		evaluate: function (ctx) {
			return [{ id: "v200", label: "", rate: { num: 1, den: 1 }, costs: [], outputs: [], items: [], valid: true }];
		}
	}`

	r := NewRegistry()
	if err := r.Put("testmod", "1.0.0", srcV1); err != nil {
		t.Fatalf("seed put: %v", err)
	}

	start := time.Now()
	errs := make(chan string, 16)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_ = r.Put("testmod", "1.0.0", srcV1)
			} else {
				_ = r.Put("testmod", "2.0.0", srcV2)
			}
		}(i)
	}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prog, ok := r.Get("testmod", "1.0.0")
			if !ok {
				return
			}
			got, err := prog.Evaluate(context.Background(), sampleContext())
			if err != nil {
				errs <- fmt.Sprintf("evaluate on a Get(\"1.0.0\") hit: %v", err)
				return
			}
			if len(got) != 1 || got[0].ID != "v100" {
				errs <- fmt.Sprintf(`Get("1.0.0") returned a program that evaluates to %+v, want one variant with ID "v100" (a torn entry paired version "1.0.0" with the "2.0.0" program)`, got)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for msg := range errs {
		t.Error(msg)
	}
	t.Logf("concurrent Put/Get (16+16 goroutines): elapsed=%s", time.Since(start))
}
