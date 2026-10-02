package solver

import (
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// makeRecipe builds a simple RecipeRow with given ID, machine, duration, item inputs and item outputs.
func makeRecipe(id, machineMod, machineID string, durationTicks int) *RecipeRow {
	return &RecipeRow{
		ID:            id,
		MachineMod:    machineMod,
		MachineID:     machineID,
		DurationTicks: durationTicks,
	}
}

func withItemInput(r *RecipeRow, modID, itemID string, amountNum, amountDen int64) *RecipeRow {
	r.Inputs = append(r.Inputs, itemIO(modID, itemID, amountNum, amountDen))
	return r
}

func withItemOutput(r *RecipeRow, modID, itemID string, amountNum, amountDen int64) *RecipeRow {
	r.Outputs = append(r.Outputs, itemIO(modID, itemID, amountNum, amountDen))
	return r
}

func item(modID, itemID string) resource.Ref {
	return resource.Ref{ModID: modID, ID: itemID}
}

// buildGraph constructs a RecipeGraph directly without a DB, for unit testing.
func buildGraph(root resource.Ref, byItemMap map[string]*RecipeRow, overrides map[string]string) *RecipeGraph {
	g := &RecipeGraph{
		Nodes:          make(map[string]*RecipeNode),
		Root:           root,
		TagResolutions: make(map[string]TagResolution),
	}
	queue := []resource.Ref{root}
	visited := make(map[string]bool)

	for qi := 0; qi < len(queue); qi++ {
		it := queue[qi]
		key := it.Key()
		if visited[key] {
			continue
		}
		visited[key] = true

		node := &RecipeNode{Item: it}

		r, ok := byItemMap[it.ModID+":"+it.ID]
		if !ok {
			node.IsRawMaterial = true
			g.Nodes[key] = node
			continue
		}

		if ov, hasOv := overrides[key]; hasOv && r.ID != ov {
			_ = ov
		}

		outputAmt, found := outputAmountFor(r, it)
		if !found {
			node.IsRawMaterial = true
			g.Nodes[key] = node
			continue
		}

		node.RecipeID = r.ID
		node.MachineMod = r.MachineMod
		node.MachineID = r.MachineID
		node.OutputAmount = outputAmt
		appendRecipeEdges(node, r, &queue)
		g.Nodes[key] = node
	}
	return g
}

// ratF converts resource.Rational to float64 for approximate comparisons in tests.
func ratF(r resource.Rational) float64 {
	if r.Den == 0 {
		return 0
	}
	return float64(r.Num) / float64(r.Den)
}

func approxEq(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

// Byproduct not needed anywhere

// Centrifuge byproduct that nothing consumes.
func TestByproduct_notNeeded(t *testing.T) {
	centrifuge := makeRecipe("r:centrifuge", "mi", "centrifuge", 20)
	withItemInput(centrifuge, "mi", "ore", 1, 1)
	withItemOutput(centrifuge, "mi", "sulfur_dust", 2, 1)
	withItemOutput(centrifuge, "mi", "silicon_dust", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:sulfur_dust": centrifuge,
	}
	root := item("mi", "sulfur_dust")
	g := buildGraph(root, byItem, nil)

	targetRate := resource.NewRational(2, 20) // 2/s = 2/20t
	rv, err := SolveDAG(g, targetRate)
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}

	recipeRate := rateFor(rv, "r:centrifuge")
	if !approxEq(ratF(recipeRate), 1.0/20, 1e-9) {
		t.Errorf("centrifuge recipeRate: got %v, want 1/20", recipeRate)
	}

	// ore demand: 1 per centrifuge run → 1/20t
	oreRate := rv.ItemRates[(&resource.Ref{ModID: "mi", ID: "ore"}).Key()]
	if !approxEq(ratF(oreRate), 1.0/20, 1e-9) {
		t.Errorf("ore itemRate: got %v, want 1/20", oreRate)
	}

	// silicon_dust has no node — not in g.Nodes, not in rv.ItemRates.
	profile := ComputeIOProfile(rv, g, FactoryState{}, "t")
	var siliconOut *IOEntry
	for i := range profile.Outputs {
		if profile.Outputs[i].Item.ID == "silicon_dust" {
			siliconOut = &profile.Outputs[i]
		}
	}
	if siliconOut == nil {
		t.Error("silicon_dust should appear as byproduct output in IOProfile")
	} else if !approxEq(ratF(siliconOut.Rate), 1.0/20, 1e-9) {
		t.Errorf("silicon_dust byproduct rate: got %v, want 1/20", siliconOut.Rate)
	}
}

// Byproduct fully covers downstream silicon_dust demand.
func TestByproduct_fullyCoversDemand(t *testing.T) {
	chemReactor := makeRecipe("r:chem", "mi", "chem_reactor", 20)
	withItemInput(chemReactor, "mi", "sulfur_dust", 1, 1)
	withItemInput(chemReactor, "mi", "ethanol", 1, 1)
	withItemInput(chemReactor, "mi", "glass", 1, 1)
	withItemOutput(chemReactor, "mi", "sulfuric_acid", 1, 1)

	centrifuge := makeRecipe("r:centrifuge", "mi", "centrifuge", 20)
	withItemInput(centrifuge, "mi", "ore", 1, 1)
	withItemOutput(centrifuge, "mi", "sulfur_dust", 1, 1)
	withItemOutput(centrifuge, "mi", "silicon_dust", 2, 1)

	furnace := makeRecipe("r:furnace", "mi", "furnace", 20)
	withItemInput(furnace, "mi", "silicon_dust", 2, 1)
	withItemOutput(furnace, "mi", "glass", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:sulfuric_acid": chemReactor,
		"mi:sulfur_dust":   centrifuge,
		"mi:glass":         furnace,
	}
	root := item("mi", "sulfuric_acid")
	g := buildGraph(root, byItem, nil)

	targetRate := resource.NewRational(1, 20)
	rv, err := SolveDAG(g, targetRate)
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}

	// Furnace must still run at 1/20t to produce glass — byproduct covers silicon_dust input.
	furnaceRate := rateFor(rv, "r:furnace")
	if !approxEq(ratF(furnaceRate), 1.0/20, 1e-9) {
		t.Errorf("glass furnace recipeRate: got %v, want 1/20", furnaceRate)
	}

	// IO balance: silicon_dust is an internal flux — must not appear as input or output.
	profile := ComputeIOProfile(rv, g, FactoryState{}, "t")
	for _, e := range profile.Inputs {
		if e.Item.ID == "silicon_dust" {
			t.Errorf("silicon_dust should not appear as input (byproduct covers it), got rate %v", e.Rate)
		}
	}
	for _, e := range profile.Outputs {
		if e.Item.ID == "silicon_dust" {
			t.Errorf("silicon_dust should not appear as output (fully consumed by furnace), got rate %v", e.Rate)
		}
	}
}

// Byproduct partially covers demand   Same chain as Test 2 but furnace needs 4 silicon_dust per run.
func TestByproduct_partiallyCoversDemand(t *testing.T) {
	chemReactor := makeRecipe("r:chem", "mi", "chem_reactor", 20)
	withItemInput(chemReactor, "mi", "sulfur_dust", 1, 1)
	withItemInput(chemReactor, "mi", "glass", 1, 1)
	withItemOutput(chemReactor, "mi", "sulfuric_acid", 1, 1)

	centrifuge := makeRecipe("r:centrifuge", "mi", "centrifuge", 20)
	withItemInput(centrifuge, "mi", "ore", 1, 1)
	withItemOutput(centrifuge, "mi", "sulfur_dust", 1, 1)
	withItemOutput(centrifuge, "mi", "silicon_dust", 2, 1)

	// Furnace needs 4 silicon_dust — twice what byproduct supplies.
	furnace := makeRecipe("r:furnace", "mi", "furnace", 20)
	withItemInput(furnace, "mi", "silicon_dust", 4, 1)
	withItemOutput(furnace, "mi", "glass", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:sulfuric_acid": chemReactor,
		"mi:sulfur_dust":   centrifuge,
		"mi:glass":         furnace,
	}
	root := item("mi", "sulfuric_acid")
	g := buildGraph(root, byItem, nil)

	targetRate := resource.NewRational(1, 20)
	rv, err := SolveDAG(g, targetRate)
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}

	// Furnace still runs at 1/20t (we need 1 glass per 20 ticks for chemReactor).
	furnaceRate := rateFor(rv, "r:furnace")
	if !approxEq(ratF(furnaceRate), 1.0/20, 1e-9) {
		t.Errorf("glass furnace recipeRate: got %v (%f), want 1/20", furnaceRate, ratF(furnaceRate))
	}

	// IO balance: silicon_dust demand = 4/20; byproduct supply = 2/20; net external = 2/20.
	profile := ComputeIOProfile(rv, g, FactoryState{}, "t")
	var siliconIn *IOEntry
	for i := range profile.Inputs {
		if profile.Inputs[i].Item.ID == "silicon_dust" {
			siliconIn = &profile.Inputs[i]
		}
	}
	if siliconIn == nil {
		t.Error("silicon_dust should appear as external input (partially covered by byproduct)")
	} else if !approxEq(ratF(siliconIn.Rate), 2.0/20, 1e-9) {
		t.Errorf("silicon_dust input rate: got %v (%f), want 2/20", siliconIn.Rate, ratF(siliconIn.Rate))
	}
}

// Byproduct is a stop point (raw material override).
func TestByproduct_stopPoint(t *testing.T) {
	centrifuge := makeRecipe("r:centrifuge", "mi", "centrifuge", 20)
	withItemInput(centrifuge, "mi", "ore", 1, 1)
	withItemOutput(centrifuge, "mi", "sulfur_dust", 1, 1)
	withItemOutput(centrifuge, "mi", "silicon_dust", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:sulfur_dust": centrifuge,
	}

	root := item("mi", "sulfur_dust")
	g := buildGraph(root, byItem, nil)

	rv, err := SolveDAG(g, resource.NewRational(1, 20))
	if err != nil {
		t.Fatalf("SolveDAG: %v", err)
	}

	// silicon_dust is not in the graph as a node (buildGraph doesn't find a recipe for it).
	profile := ComputeIOProfile(rv, g, FactoryState{}, "t")
	found := false
	for _, o := range profile.Outputs {
		if o.Item.ID == "silicon_dust" {
			found = true
			if !approxEq(ratF(o.Rate), 1.0/20, 1e-9) {
				t.Errorf("silicon_dust byproduct rate: got %f, want %f", ratF(o.Rate), 1.0/20)
			}
		}
	}
	if !found {
		t.Error("silicon_dust should appear as byproduct output in IOProfile")
	}
}

// Linear system with byproduct   Same chain as Test 2 via the stoichiometry matrix path.
func TestByproduct_linearSystem(t *testing.T) {
	chemReactor := makeRecipe("r:chem", "mi", "chem_reactor", 20)
	withItemInput(chemReactor, "mi", "sulfur_dust", 1, 1)
	withItemInput(chemReactor, "mi", "glass", 1, 1)
	withItemOutput(chemReactor, "mi", "sulfuric_acid", 1, 1)

	centrifuge := makeRecipe("r:centrifuge", "mi", "centrifuge", 20)
	withItemInput(centrifuge, "mi", "ore", 1, 1)
	withItemOutput(centrifuge, "mi", "sulfur_dust", 1, 1)
	withItemOutput(centrifuge, "mi", "silicon_dust", 2, 1)

	furnace := makeRecipe("r:furnace", "mi", "furnace", 20)
	withItemInput(furnace, "mi", "silicon_dust", 2, 1)
	withItemOutput(furnace, "mi", "glass", 1, 1)

	byItem := map[string]*RecipeRow{
		"mi:sulfuric_acid": chemReactor,
		"mi:sulfur_dust":   centrifuge,
		"mi:glass":         furnace,
	}
	root := item("mi", "sulfuric_acid")
	g := buildGraph(root, byItem, nil)

	targetRate := resource.NewRational(1, 20)
	S, items, recipeIDs := BuildStoichiometryMatrix(g)

	b := make([]resource.Rational, len(items))
	for i, it := range items {
		if it.Key() == root.Key() {
			b[i] = targetRate
		}
	}

	x, err := GaussJordanRational(S, b)
	if err != nil {
		t.Fatalf("GaussJordanRational: %v", err)
	}

	rv := newRateVector()
	for i, rid := range recipeIDs {
		rv.RecipeRates[rid] = x[i]
	}
	for i, it := range items {
		net := resource.NewRational(0, 1)
		for j := range recipeIDs {
			net = net.Add(S[i][j].Mul(x[j]))
		}
		rv.ItemRates[it.Key()] = net
		_ = i
	}

	// Furnace must run at 1/20t — it's the only source of glass.
	furnaceRate := rateFor(rv, "r:furnace")
	if !approxEq(ratF(furnaceRate), 1.0/20, 1e-9) {
		t.Errorf("linear: glass furnace rate should be 1/20, got %v (%f)", furnaceRate, ratF(furnaceRate))
	}

	// IO balance should show silicon_dust as neither input nor output (net zero).
	profile := ComputeIOProfile(rv, g, FactoryState{}, "t")
	for _, e := range profile.Inputs {
		if e.Item.ID == "silicon_dust" {
			t.Errorf("silicon_dust should not appear as input, got rate %v", e.Rate)
		}
	}
	for _, e := range profile.Outputs {
		if e.Item.ID == "silicon_dust" {
			t.Errorf("silicon_dust should not appear as output, got rate %v", e.Rate)
		}
	}
}
