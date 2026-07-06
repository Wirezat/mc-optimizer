package solver

// Comprehensive solver tests covering all relevant recipe constellations.
// Uses buildGraph + SolveDAG / SolveLinearSystem / ComputeIOProfile directly
// (no DB required) via the test helpers in byproduct_test.go.

import (
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────

func solveBoth(t *testing.T, g *RecipeGraph, target Rational) (dag RateVector, lin RateVector) {
	t.Helper()
	var err error
	dag, err = SolveDAG(g, target)
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}
	lin, err = SolveLinearSystem(g, target)
	if err != nil {
		t.Fatalf("SolveLinearSystem: %v", err)
	}
	return dag, lin
}

// rateFor looks up a rate by bare recipe ID, ignoring which machine it landed
// on — RateVector is keyed by RecipeOptionKey(recipe, machine) now, but these
// fixture tests only ever attach one machine per synthetic recipe id.
func rateFor(rv RateVector, recipeID string) Rational {
	for k, v := range rv.RecipeRates {
		if id, _, _, ok := ParseRecipeOptionKey(k); ok && id == recipeID {
			return v
		}
	}
	return Rational{}
}

func assertRate(t *testing.T, label string, got Rational, wantNum, wantDen int64) {
	t.Helper()
	want := float64(wantNum) / float64(wantDen)
	if !approxEq(ratF(got), want, 1e-9) {
		t.Errorf("%s: got %v (%f), want %d/%d (%f)", label, got, ratF(got), wantNum, wantDen, want)
	}
}

func assertDagLinMatch(t *testing.T, label string, dag, lin RateVector, recipeID string) {
	t.Helper()
	d := rateFor(dag, recipeID)
	l := rateFor(lin, recipeID)
	if !approxEq(ratF(d), ratF(l), 1e-9) {
		t.Errorf("%s DAG vs linalg mismatch for %s: dag=%v lin=%v", label, recipeID, d, l)
	}
}

func ioHasInput(profile IOProfile, itemID string) *IOEntry {
	for i := range profile.Inputs {
		if profile.Inputs[i].Item.ItemID == itemID {
			return &profile.Inputs[i]
		}
	}
	return nil
}

func ioHasOutput(profile IOProfile, itemID string) *IOEntry {
	for i := range profile.Outputs {
		if profile.Outputs[i].Item.ItemID == itemID {
			return &profile.Outputs[i]
		}
	}
	return nil
}

// ── 1. Linear chain ───────────────────────────────────────────────────────
//
// steel → [assembler, 20t] → gear (1 per run)
// iron  → [furnace,   20t] → steel (2 per run)
// iron is raw material.
//
// Target: 4 gear/t (=4/1 per tick)
//   gear rate: 4/1 per tick → assembler at 4/1 rate, needs 4 steel/t
//   steel: 4/t; furnace outputs 2/run → runs at 2/t, needs 2 iron/t

func TestChain_linear(t *testing.T) {
	furnace := makeRecipe("r:furnace", "mc", "furnace", 20)
	withItemInput(furnace, "mc", "iron", 1, 1)
	withItemOutput(furnace, "mc", "steel", 2, 1)

	assembler := makeRecipe("r:assembler", "mc", "assembler", 20)
	withItemInput(assembler, "mc", "steel", 1, 1)
	withItemOutput(assembler, "mc", "gear", 1, 1)

	byItem := map[string]*RecipeRow{
		"mc:gear":  assembler,
		"mc:steel": furnace,
	}
	g := buildGraph(item("mc", "gear"), byItem, nil)
	target := NewRational(4, 1)

	dag, lin := solveBoth(t, g, target)

	// Recipe rates must agree for both solvers.
	assertRate(t, "dag assembler", rateFor(dag, "r:assembler"), 4, 1)
	assertRate(t, "dag furnace", rateFor(dag, "r:furnace"), 2, 1)
	assertRate(t, "lin assembler", rateFor(lin, "r:assembler"), 4, 1)
	assertRate(t, "lin furnace", rateFor(lin, "r:furnace"), 2, 1)
	assertDagLinMatch(t, "linear chain", dag, lin, "r:assembler")
	assertDagLinMatch(t, "linear chain", dag, lin, "r:furnace")

	// DAG propagates raw-material item rates; verify via IO profile (works for both).
	dagProfile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	iron := ioHasInput(dagProfile, "iron")
	if iron == nil {
		t.Error("iron must appear as input")
	} else {
		assertRate(t, "IO iron", iron.Rate, 2, 1)
	}
	gear := ioHasOutput(dagProfile, "gear")
	if gear == nil {
		t.Error("gear must appear as output")
	} else {
		assertRate(t, "IO gear", gear.Rate, 4, 1)
	}
}

// ── 2. Shared intermediate ────────────────────────────────────────────────
//
// A and B both need C. C has its own recipe from D (raw).
//   C ← [machine_c, 20t] ← D (1 per run → 3 C)
//   A ← [machine_a, 20t] ← 1 C (1 per run → 1 A)
//   B ← [machine_b, 20t] ← 2 C (1 per run → 1 B)
//   root ← [machine_r, 20t] ← 1 A + 1 B (→ 1 root)
//
// Target: 1 root/tick
//   root runs at 1/t → needs 1 A + 1 B per tick
//   A machine at 1/t, needs 1 C/t
//   B machine at 1/t, needs 2 C/t
//   Total C demand: 3/t → C machine at 1/t (produces 3/run), needs 1 D/t

func TestChain_sharedIntermediate(t *testing.T) {
	machC := makeRecipe("r:c", "mi", "mc", 20)
	withItemInput(machC, "mi", "d", 1, 1)
	withItemOutput(machC, "mi", "c", 3, 1)

	machA := makeRecipe("r:a", "mi", "ma", 20)
	withItemInput(machA, "mi", "c", 1, 1)
	withItemOutput(machA, "mi", "a", 1, 1)

	machB := makeRecipe("r:b", "mi", "mb", 20)
	withItemInput(machB, "mi", "c", 2, 1)
	withItemOutput(machB, "mi", "b", 1, 1)

	machR := makeRecipe("r:root", "mi", "mr", 20)
	withItemInput(machR, "mi", "a", 1, 1)
	withItemInput(machR, "mi", "b", 1, 1)
	withItemOutput(machR, "mi", "root", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:root": machR,
		"mi:a":    machA,
		"mi:b":    machB,
		"mi:c":    machC,
	}
	g := buildGraph(item("mi", "root"), byItem, nil)
	target := NewRational(1, 1)

	dag, lin := solveBoth(t, g, target)

	for _, label := range []string{"dag", "lin"} {
		rv := dag
		if label == "lin" {
			rv = lin
		}
		assertRate(t, label+" r:root", rateFor(rv, "r:root"), 1, 1)
		assertRate(t, label+" r:a", rateFor(rv, "r:a"), 1, 1)
		assertRate(t, label+" r:b", rateFor(rv, "r:b"), 1, 1)
		assertRate(t, label+" r:c", rateFor(rv, "r:c"), 1, 1)
	}
	for _, id := range []string{"r:root", "r:a", "r:b", "r:c"} {
		assertDagLinMatch(t, "sharedIntermediate", dag, lin, id)
	}

	// Raw material rates via IO profile (linalg doesn't populate ItemRates for raws).
	profile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	d := ioHasInput(profile, "d")
	if d == nil {
		t.Error("d must appear as input")
	} else {
		assertRate(t, "IO d", d.Rate, 1, 1)
	}
	// c is internal — must not appear in IO
	if ioHasInput(profile, "c") != nil {
		t.Error("c must not appear as external input")
	}
}

// ── 3. Probability < 1 on output ─────────────────────────────────────────
//
// Centrifuge: 1 ore → 1 dust (prob 1) + 1 gem (prob 1/2)
// Target: 1 gem/t. Centrifuge must run at 2/t (half the runs yield gem).
// Dust appears as byproduct output at 2/t (produced every run).

func TestChain_probabilisticOutput(t *testing.T) {
	centrifuge := makeRecipe("r:centrifuge", "mi", "centrifuge", 20)
	withItemInput(centrifuge, "mi", "ore", 1, 1)
	// primary output: gem, prob 1/2
	centrifuge.ItemOutputs = append(centrifuge.ItemOutputs, RecipeRowItemIO{
		ItemModID: mod("mi"), ItemID: mod("gem"),
		AmountNum: 1, AmountDen: 1,
		ProbabilityNum: 1, ProbabilityDen: 2,
	})
	// secondary output: dust, prob 1 (always)
	centrifuge.ItemOutputs = append(centrifuge.ItemOutputs, RecipeRowItemIO{
		ItemModID: mod("mi"), ItemID: mod("dust"),
		AmountNum: 1, AmountDen: 1,
		ProbabilityNum: 1, ProbabilityDen: 1,
	})

	byItem := map[string]*RecipeRow{
		"mi:gem": centrifuge,
	}
	g := buildGraph(item("mi", "gem"), byItem, nil)

	// OutputAmount for gem = 1 * 1/2 = 1/2
	// To get 1 gem/t: centrifuge rate = 1 / (1/2) = 2/t
	// ore demand = 2/t; dust byproduct = 2/t

	target := NewRational(1, 1)
	dag, err := SolveDAG(g, target)
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}

	assertRate(t, "centrifuge rate", rateFor(dag, "r:centrifuge"), 2, 1)
	assertRate(t, "ore demand", dag.ItemRates["mi:ore"], 2, 1)

	profile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	dust := ioHasOutput(profile, "dust")
	if dust == nil {
		t.Error("dust should appear as byproduct output")
	} else {
		assertRate(t, "dust byproduct rate", dust.Rate, 2, 1)
	}
}

// ── 4. Stop-point in the middle of the chain ─────────────────────────────
//
// Chain: root ← mid ← leaf
// mid is declared a stop-point → treated as externally supplied.
// Expected: only root recipe runs; mid and leaf don't appear as recipes.
// mid appears as external INPUT in IO balance.

func TestChain_stopPoint(t *testing.T) {
	recipeLeaf := makeRecipe("r:leaf", "mi", "m", 20)
	withItemInput(recipeLeaf, "mi", "raw", 1, 1)
	withItemOutput(recipeLeaf, "mi", "mid", 1, 1)

	recipeMid := makeRecipe("r:root", "mi", "m", 20)
	withItemInput(recipeMid, "mi", "mid", 2, 1)
	withItemOutput(recipeMid, "mi", "root", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:root": recipeMid,
		"mi:mid":  recipeLeaf,
	}
	g := buildGraph(item("mi", "root"), byItem, nil)

	// Mark mid as stop-point after graph construction
	if node, ok := g.Nodes["mi:mid"]; ok {
		node.IsStopPoint = true
		node.RecipeID = ""  // remove recipe — stop-point is externally supplied
		node.Inputs = nil
	}
	// Remove leaf node (not reachable through stop-point BFS)
	delete(g.Nodes, "mi:raw")

	target := NewRational(1, 1)
	dag, err := SolveDAG(g, target)
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}

	assertRate(t, "root recipe", rateFor(dag, "r:root"), 1, 1)
	// leaf recipe should not run (mid is stop-point)
	if r := rateFor(dag, "r:leaf"); r.Num != 0 {
		t.Errorf("leaf recipe should not run (mid is stop-point), got %v", r)
	}

	profile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	mid := ioHasInput(profile, "mid")
	if mid == nil {
		t.Error("mid should appear as external input (stop-point)")
	} else {
		assertRate(t, "mid input rate", mid.Rate, 2, 1)
	}
}

// ── 5. Factory-provided item ─────────────────────────────────────────────
//
// Same as stop-point but uses IsFactoryProvided flag.
// Expected: no recipe runs for the provided item; it appears as input in IO balance.

func TestChain_factoryProvided(t *testing.T) {
	recipeRoot := makeRecipe("r:root", "mi", "m", 20)
	withItemInput(recipeRoot, "mi", "provided", 3, 1)
	withItemOutput(recipeRoot, "mi", "root", 1, 1)

	recipeProvided := makeRecipe("r:provided", "mi", "m", 20)
	withItemInput(recipeProvided, "mi", "raw", 1, 1)
	withItemOutput(recipeProvided, "mi", "provided", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:root":     recipeRoot,
		"mi:provided": recipeProvided,
	}
	g := buildGraph(item("mi", "root"), byItem, nil)

	// Mark provided as factory-provided after build
	if node, ok := g.Nodes["mi:provided"]; ok {
		node.IsFactoryProvided = true
		node.IsRawMaterial = false
		node.RecipeID = ""
		node.Inputs = nil
	}
	delete(g.Nodes, "mi:raw")

	target := NewRational(1, 1)
	dag, err := SolveDAG(g, target)
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}

	assertRate(t, "root recipe", rateFor(dag, "r:root"), 1, 1)
	if r := rateFor(dag, "r:provided"); r.Num != 0 {
		t.Errorf("provided recipe should not run, got %v", r)
	}

	profile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	prov := ioHasInput(profile, "provided")
	if prov == nil {
		t.Error("provided item should appear as input")
	} else {
		assertRate(t, "provided input rate", prov.Rate, 3, 1)
		if !prov.IsFactoryProvided {
			t.Error("provided item should have IsFactoryProvided=true")
		}
	}
}

// ── 6. Multiple byproducts, both consumed downstream ─────────────────────
//
// smelter: 2 ore → 1 iron + 1 slag (byproduct)
// slag_press: 1 slag → 1 plate
// assembler: 1 iron + 1 plate → 1 gear (root)
//
// Target: 1 gear/t
//   assembler at 1/t needs 1 iron + 1 plate
//   iron: smelter at 1/t produces 1 iron + 1 slag as byproduct
//   plate: slag_press at 1/t needs 1 slag — smelter byproduct covers exactly
//   slag: internal (produced 1/t, consumed 1/t) → not in IO balance
//   external inputs: only ore (2/t)

func TestChain_multipleByproducts_bothConsumed(t *testing.T) {
	smelter := makeRecipe("r:smelter", "mi", "smelter", 20)
	withItemInput(smelter, "mi", "ore", 2, 1)
	withItemOutput(smelter, "mi", "iron", 1, 1)
	withItemOutput(smelter, "mi", "slag", 1, 1)

	slagPress := makeRecipe("r:slag_press", "mi", "press", 20)
	withItemInput(slagPress, "mi", "slag", 1, 1)
	withItemOutput(slagPress, "mi", "plate", 1, 1)

	assembler := makeRecipe("r:assembler", "mi", "assembler", 20)
	withItemInput(assembler, "mi", "iron", 1, 1)
	withItemInput(assembler, "mi", "plate", 1, 1)
	withItemOutput(assembler, "mi", "gear", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:gear":  assembler,
		"mi:iron":  smelter,
		"mi:plate": slagPress,
	}
	g := buildGraph(item("mi", "gear"), byItem, nil)
	target := NewRational(1, 1)

	dag, lin := solveBoth(t, g, target)

	for _, label := range []string{"dag", "lin"} {
		rv := dag
		if label == "lin" {
			rv = lin
		}
		assertRate(t, label+" assembler", rateFor(rv, "r:assembler"), 1, 1)
		assertRate(t, label+" smelter", rateFor(rv, "r:smelter"), 1, 1)
		assertRate(t, label+" slag_press", rateFor(rv, "r:slag_press"), 1, 1)
	}
	for _, id := range []string{"r:assembler", "r:smelter", "r:slag_press"} {
		assertDagLinMatch(t, "multiByproduct", dag, lin, id)
	}

	profile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	// ore: only external input
	ore := ioHasInput(profile, "ore")
	if ore == nil {
		t.Error("ore must appear as input")
	} else {
		assertRate(t, "IO ore", ore.Rate, 2, 1)
	}
	// slag: internal flux — must not appear
	if ioHasInput(profile, "slag") != nil {
		t.Error("slag must not appear as external input (covered by smelter byproduct)")
	}
	if ioHasOutput(profile, "slag") != nil {
		t.Error("slag must not appear as output (consumed by slag_press)")
	}
}

// ── 7. Byproduct excess: more produced than consumed ─────────────────────
//
// smelter: 1 ore → 1 iron + 3 slag
// only 1 slag consumed by slag_press per gear
// assembler: 1 iron + 1 plate → 1 gear
//
// slag produced: 3/t; consumed: 1/t → net export: 2/t in IO outputs

func TestChain_byproductExcess(t *testing.T) {
	smelter := makeRecipe("r:smelter", "mi", "smelter", 20)
	withItemInput(smelter, "mi", "ore", 1, 1)
	withItemOutput(smelter, "mi", "iron", 1, 1)
	withItemOutput(smelter, "mi", "slag", 3, 1)

	slagPress := makeRecipe("r:slag_press", "mi", "press", 20)
	withItemInput(slagPress, "mi", "slag", 1, 1)
	withItemOutput(slagPress, "mi", "plate", 1, 1)

	assembler := makeRecipe("r:assembler", "mi", "assembler", 20)
	withItemInput(assembler, "mi", "iron", 1, 1)
	withItemInput(assembler, "mi", "plate", 1, 1)
	withItemOutput(assembler, "mi", "gear", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:gear":  assembler,
		"mi:iron":  smelter,
		"mi:plate": slagPress,
	}
	g := buildGraph(item("mi", "gear"), byItem, nil)
	target := NewRational(1, 1)

	dag, lin := solveBoth(t, g, target)
	for _, id := range []string{"r:assembler", "r:smelter", "r:slag_press"} {
		assertDagLinMatch(t, "byproductExcess", dag, lin, id)
	}

	profile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	// net slag = 3 produced - 1 consumed = 2 exported
	slagOut := ioHasOutput(profile, "slag")
	if slagOut == nil {
		t.Error("slag should appear as excess byproduct output")
	} else {
		assertRate(t, "slag net export", slagOut.Rate, 2, 1)
	}
	// slag must not appear as input
	if ioHasInput(profile, "slag") != nil {
		t.Error("slag must not appear as external input")
	}
}

// ── 8. Cyclic graph (only linalg) ─────────────────────────────────────────
//
// Nuclear fuel cycle:
//   centrifuge:  1 depleted_cell + water → 1 enriched_uranium
//   reactor:     1 enriched_uranium + coolant → 2 depleted_cell  (surplus: 1 per run)
//   packager:    1 depleted_cell → 1 fuel_rod  (uses the surplus depleted_cell)
//
// Cycle: enriched_uranium ↔ depleted_cell (balanced 1:1 between centrifuge and reactor)
// Reactor produces 2 depleted_cells per run: 1 goes back to centrifuge, 1 goes to packager.
// Matrix rows:
//   fuel_rod:         packager=+1 = target (1/t)
//   depleted_cell:    packager=-1, reactor=+2, centrifuge=-1 = 0
//   enriched_uranium: reactor=-1, centrifuge=+1 = 0
// Solution: packager=1, centrifuge=reactor=1 (all run at equal rate).
//
// SolveDAG must FAIL; SolveLinearSystem must succeed.

func TestCycle_linearOnly(t *testing.T) {
	centrifuge := makeRecipe("r:centrifuge", "mi", "centrifuge", 100)
	withItemInput(centrifuge, "mi", "depleted_cell", 1, 1)
	withItemInput(centrifuge, "mi", "water", 10, 1)
	withItemOutput(centrifuge, "mi", "enriched_uranium", 1, 1)

	reactor := makeRecipe("r:reactor", "mi", "reactor", 100)
	withItemInput(reactor, "mi", "enriched_uranium", 1, 1)
	withItemInput(reactor, "mi", "coolant", 5, 1)
	withItemOutput(reactor, "mi", "depleted_cell", 2, 1) // 2 per run — surplus feeds packager

	packager := makeRecipe("r:packager", "mi", "packager", 20)
	withItemInput(packager, "mi", "depleted_cell", 1, 1)
	withItemOutput(packager, "mi", "fuel_rod", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:fuel_rod":         packager,
		"mi:depleted_cell":    reactor,
		"mi:enriched_uranium": centrifuge,
	}
	g := buildGraph(item("mi", "fuel_rod"), byItem, nil)

	_, hasCycle := DetectCycles(g)
	if !hasCycle {
		t.Fatal("expected cycle to be detected in this graph")
	}

	_, dagErr := SolveDAG(g, NewRational(1, 1))
	if dagErr == nil {
		t.Error("SolveDAG should fail on cyclic graph")
	}

	rv, err := SolveLinearSystem(g, NewRational(1, 1))
	if err != nil {
		t.Fatalf("SolveLinearSystem on cyclic graph: %v", err)
	}

	// packager=1, reactor=centrifuge=1 (cycle balanced)
	assertRate(t, "packager", rateFor(rv, "r:packager"), 1, 1)
	assertRate(t, "reactor", rateFor(rv, "r:reactor"), 1, 1)
	assertRate(t, "centrifuge", rateFor(rv, "r:centrifuge"), 1, 1)
}

// ── 9. DAG and linalg produce identical results (acyclic) ─────────────────
//
// Multi-step acyclic chain with a shared intermediate.
// Both solvers must return the same recipe rates to within floating-point precision.

func TestSolvers_dagLinalgParity(t *testing.T) {
	// plastic ← chemical_plant ← ethylene + chlorine (raws)
	chem := makeRecipe("r:chem", "mi", "chem", 40)
	withItemInput(chem, "mi", "ethylene", 2, 1)
	withItemInput(chem, "mi", "chlorine", 1, 1)
	withItemOutput(chem, "mi", "plastic", 3, 1)

	// circuit ← assembler ← plastic + copper_wire (raw)
	assemblerC := makeRecipe("r:circuit", "mi", "assembler", 20)
	withItemInput(assemblerC, "mi", "plastic", 2, 1)
	withItemInput(assemblerC, "mi", "copper_wire", 3, 1)
	withItemOutput(assemblerC, "mi", "circuit", 1, 1)

	// machine ← fabricator ← circuit + steel (raw)
	fabricator := makeRecipe("r:machine", "mi", "fabricator", 60)
	withItemInput(fabricator, "mi", "circuit", 4, 1)
	withItemInput(fabricator, "mi", "steel", 2, 1)
	withItemOutput(fabricator, "mi", "machine", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:machine": fabricator,
		"mi:circuit": assemblerC,
		"mi:plastic": chem,
	}
	g := buildGraph(item("mi", "machine"), byItem, nil)

	_, hasCycle := DetectCycles(g)
	if hasCycle {
		t.Fatal("test graph should be acyclic")
	}

	target := NewRational(1, 1)
	dag, lin := solveBoth(t, g, target)

	for _, id := range []string{"r:machine", "r:circuit", "r:chem"} {
		d, l := ratF(dag.RecipeRates[id]), ratF(lin.RecipeRates[id])
		if !approxEq(d, l, 1e-9) {
			t.Errorf("DAG/linalg diverge for %s: dag=%f lin=%f", id, d, l)
		}
	}

	// machine at 1/t → needs 4 circuits/t → circuit machine at 4/t, needs 8 plastic/t
	// plastic at 8/t, chem outputs 3/run → chem at 8/3/t
	assertRate(t, "fabricator", rateFor(dag, "r:machine"), 1, 1)
	assertRate(t, "circuit assembler", rateFor(dag, "r:circuit"), 4, 1)
	assertRate(t, "chem plant", rateFor(dag, "r:chem"), 8, 3)
}

// ── 10. Zero-rate recipe when item is fully stop-pointed ──────────────────
//
// If an item has a recipe but is declared as a stop-point, the recipe must
// not appear in the rate vector at all (or be zero).

func TestChain_stopPointZerosRecipe(t *testing.T) {
	recipeA := makeRecipe("r:a", "mi", "m", 20)
	withItemInput(recipeA, "mi", "raw", 1, 1)
	withItemOutput(recipeA, "mi", "a", 1, 1)

	recipeRoot := makeRecipe("r:root", "mi", "m", 20)
	withItemInput(recipeRoot, "mi", "a", 1, 1)
	withItemOutput(recipeRoot, "mi", "root", 1, 1)

	// Build graph with a as stop-point from the start
	g := &RecipeGraph{
		Nodes:          make(map[string]*RecipeNode),
		Root:           item("mi", "root"),
		TagResolutions: make(map[string]TagResolution),
	}
	// root node
	rootNode := &RecipeNode{
		Item: item("mi", "root"), RecipeID: "r:root",
		MachineMod: "mi", MachineID: "m",
		OutputAmount: NewRational(1, 1),
	}
	appendRecipeEdges(rootNode, recipeRoot, &[]ItemRef{})
	g.Nodes["mi:root"] = rootNode

	// a as stop-point
	g.Nodes["mi:a"] = &RecipeNode{
		Item:        item("mi", "a"),
		IsStopPoint: true,
	}

	dag, err := SolveDAG(g, NewRational(1, 1))
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}
	if r := rateFor(dag, "r:a"); r.Num != 0 {
		t.Errorf("r:a should not run (a is stop-point), got %v", r)
	}
	assertRate(t, "root recipe", rateFor(dag, "r:root"), 1, 1)

	profile := ComputeIOProfile(dag, g, FactoryState{}, "t")
	aInput := ioHasInput(profile, "a")
	if aInput == nil {
		t.Error("a should appear as stop-point input in IO balance")
	} else if !aInput.IsStopPoint {
		t.Error("a IO entry should have IsStopPoint=true")
	}
}
