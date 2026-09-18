package plugins

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// The timeout/pool design must ensure plugins that panic, hang, or return oversized results
// fail cleanly without crashing the host, hanging callers, or exhausting memory.

// TestEvaluateSurvivesTimeoutThenSucceeds asserts a Program keeps working correctly after
// one call on it times out, and - critically - that the call following a timeout cannot
// observe state a timed-out call mutated.
func TestEvaluateSurvivesTimeoutThenSucceeds(t *testing.T) {
	prog, err := Compile("testmod", `var counter = 0;
	var plugin = {
		evaluate: function (ctx) {
			counter++;
			if (ctx.recipe.duration_ticks < 0) {
				while (true) {}
			}
			return [{
				id: "call" + counter, label: "", rate: { num: 1, den: 1 },
				costs: [], outputs: [], items: [], valid: true
			}]
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	timedOut := sampleContext()
	timedOut.Recipe.DurationTicks = -1
	start := time.Now()
	if _, err := prog.Evaluate(context.Background(), timedOut); err == nil {
		t.Fatal("want error from endless-loop call, got nil")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timed-out call took %s, want close to evalTimeout=%s", elapsed, evalTimeout)
	}

	ok := sampleContext()
	got, err := prog.Evaluate(context.Background(), ok)
	if err != nil {
		t.Fatalf("evaluate after a prior timeout: %v", err)
	}
	if len(got) != 1 || got[0].ID != "call1" {
		t.Fatalf("got %+v, want one variant with ID %q (a fresh VM); %q would mean the timed-out call's top-level state leaked into this one", got, "call1", "call2")
	}
}

// TestEvaluateRecoversFromThrowingGetter asserts that when a property getter throws while
// walking a result, Evaluate catches it as an error instead of allowing a process-level
// panic.
func TestEvaluateRecoversFromThrowingGetter(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var o = { label: "", rate: { num: 1, den: 1 }, costs: [], outputs: [], items: [], valid: true };
			Object.defineProperty(o, "id", { get: function () { throw new Error("boom"); }, enumerable: true });
			return [o];
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if _, err := prog.Evaluate(context.Background(), sampleContext()); err == nil {
		t.Fatal("want error from a throwing getter, got nil")
	}
	// Reaching this line at all proves Evaluate did not panic; a discarded (poisoned) instance
	// must not stop the Program from being usable again.
	prog2, err := Compile("testmod", `var plugin = { evaluate: function(ctx) {
		return [{ id: "base", label: "", rate: { num: 1, den: 1 }, costs: [], outputs: [], items: [], valid: true }];
	} }`)
	if err != nil {
		t.Fatalf("compile second program: %v", err)
	}
	if _, err := prog2.Evaluate(context.Background(), sampleContext()); err != nil {
		t.Fatalf("unrelated program failed after a panic elsewhere: %v", err)
	}
}

// TestCompileRejectsTopLevelInfiniteLoop asserts that top-level plugin code (run once per
// VM, including lazily inside the pool) is bounded by the same guard as evaluate() itself,
// instead of hanging Compile's caller forever.
func TestCompileRejectsTopLevelInfiniteLoop(t *testing.T) {
	start := time.Now()
	_, err := Compile("testmod", "while (true) {}\nvar plugin = { evaluate: function(ctx) { return []; } }")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error for a plugin whose top-level code never finishes, got nil")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Compile took %s, want close to evalTimeout=%s", elapsed, evalTimeout)
	}
}

// TestEvaluateRejectsOversizedResultWithoutExporting asserts that a result over maxVariants
// is rejected by reading the JS array's length before ExportTo would walk and allocate a Go
// struct per element.
func TestEvaluateRejectsOversizedResultWithoutExporting(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var o = [];
			o.length = ctx.recipe.duration_ticks;
			return o;
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	ec := sampleContext()
	ec.Recipe.DurationTicks = 100_000

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	start := time.Now()
	_, err = prog.Evaluate(context.Background(), ec)
	elapsed := time.Since(start)

	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("want error for an oversized result, got nil")
	}
	if elapsed >= evalTimeout {
		t.Fatalf("rejecting a sparse length=100000 array took %s, want well under evalTimeout=%s (may indicate the interrupt fired instead of the length check)", elapsed, evalTimeout)
	}
	// A bound clearly below what reflect.MakeSlice(typ, 100000, 100000) for []Variant would
	// allocate on its own (well over a megabyte, see the mutation probe recorded in the fix
	// report), while generous enough for the O(1) length write and the length check itself.
	const ceiling = 256 << 10 // 256 KiB
	delta := after.TotalAlloc - before.TotalAlloc
	if delta > ceiling {
		t.Fatalf("rejecting an oversized result allocated %d bytes, want under %d (ExportTo may be running before the length check)", delta, ceiling)
	}
	t.Logf("oversized-result rejection: elapsed=%s allocated=%d bytes", elapsed, delta)
}

// TestEvaluateRejectsLyingLengthGetter verifies that a plain object whose length getter
// lies between two reads is rejected; 648 MiB was measured when the getter returned 1 then
// 5000000.
func TestEvaluateRejectsLyingLengthGetter(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			var calls = 0;
			var o = {};
			Object.defineProperty(o, "length", {
				get: function () { calls++; return calls === 1 ? 1 : 5000000; },
				enumerable: true
			});
			return o;
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	_, err = prog.Evaluate(context.Background(), sampleContext())

	runtime.ReadMemStats(&after)
	delta := after.TotalAlloc - before.TotalAlloc

	if err == nil {
		t.Fatal("want error for a plugin whose length getter lies between reads, got nil")
	}
	const ceiling = 1 << 20 // 1 MiB
	if delta > ceiling {
		t.Fatalf("rejecting a lying-length-getter result allocated %d bytes, want under %d (the length-oracle bypass may still be open)", delta, ceiling)
	}
	t.Logf("lying-length-getter rejection: allocated=%d bytes", delta)
}

// TestHardenedGlobalsAreDeterministic asserts Date and Math.random are unreachable from
// plugin code, since results are cached by config hash and a non-deterministic plugin would
// freeze an arbitrary value forever.
func TestHardenedGlobalsAreDeterministic(t *testing.T) {
	prog, err := Compile("testmod", `var plugin = {
		evaluate: function (ctx) {
			return [{
				id: (typeof Date === "undefined" && typeof Math.random === "undefined") ? "hardened" : "leaky",
				label: "", rate: { num: 1, den: 1 }, costs: [], outputs: [], items: [], valid: true
			}]
		}
	}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := prog.Evaluate(context.Background(), sampleContext())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(got) != 1 || got[0].ID != "hardened" {
		t.Fatalf("got ID %q, want %q: Date or Math.random is still reachable from plugin code", got[0].ID, "hardened")
	}
}
