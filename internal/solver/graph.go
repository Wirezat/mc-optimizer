package solver

import (
	"context"
	"fmt"
	"strings"
)

// Edge represents a directed connection between a recipe node and an item or fluid.
type Edge struct {
	Item        ItemRef  // empty if IsFluid
	ModID       string   // fluid mod (IsFluid only)
	FluidID     string   // (IsFluid only)
	AmountMB    int64    // (IsFluid only)
	Amount      Rational // (item only)
	Probability Rational
	IsFluid     bool
}

// RecipeNode represents a single item in the recipe graph, optionally bound to a specific recipe.
type RecipeNode struct {
	Item              ItemRef
	RecipeID          string
	MachineMod        string
	MachineID         string
	Inputs            []Edge
	Outputs           []Edge
	OutputAmount      Rational
	IsStopPoint       bool
	IsRawMaterial     bool
	IsFactoryProvided bool
}

// RateKey identifies this node's (recipe, machine) pair for rate-tracking purposes.
// A bare RecipeID is NOT enough: tier variants (e.g. a bronze machine implementing
// an electric base type via machine_interfaces) share the same RecipeID but must be
// tracked — and later costed — as distinct machine choices.
func (n *RecipeNode) RateKey() string {
	return RecipeOptionKey(n.RecipeID, n.MachineMod, n.MachineID)
}

// RecipeOptionKey uniquely identifies a (recipe, machine) choice. Recipes may be
// reachable via more than one machine (tier variants sharing a recipe row through
// machine_interfaces), so the recipe ID alone cannot disambiguate which machine
// was actually chosen to run it.
func RecipeOptionKey(recipeID, machineMod, machineID string) string {
	return recipeID + "@" + machineMod + ":" + machineID
}

// ParseRecipeOptionKey splits a RecipeOptionKey back into its parts.
func ParseRecipeOptionKey(key string) (recipeID, machineMod, machineID string, ok bool) {
	recipeID, rest, ok := strings.Cut(key, "@")
	if !ok {
		return "", "", "", false
	}
	machineMod, machineID, ok = strings.Cut(rest, ":")
	if !ok {
		return "", "", "", false
	}
	return recipeID, machineMod, machineID, true
}

// RecipeGraph maps item keys to RecipeNode and stores the root item.
type RecipeGraph struct {
	Nodes          map[string]*RecipeNode
	Root           ItemRef
	TagResolutions map[string]TagResolution // tagKey → resolved item + options
}

// BuildRecipeGraph constructs a directed recipe graph starting from the root item.
// It stops at stop points, factory-provided items, or raw materials (no recipe).
func (s *Solver) BuildRecipeGraph(
	ctx context.Context,
	root ItemRef,
	stopPoints map[string]bool,
	factory FactoryState,
	overrides map[string]string,
	tagOverrides map[string]string,
) (*RecipeGraph, error) {
	g := &RecipeGraph{
		Nodes:          make(map[string]*RecipeNode),
		Root:           root,
		TagResolutions: make(map[string]TagResolution),
	}
	queue := []ItemRef{root}
	visited := make(map[string]bool)

	for qi := 0; qi < len(queue); qi++ {
		item := queue[qi]
		key := item.Key()
		if visited[key] {
			continue
		}
		visited[key] = true

		node := &RecipeNode{Item: item}

		if stopPoints[key] {
			node.IsStopPoint = true
			g.Nodes[key] = node
			continue
		}

		if _, ok := factory.ExistingOutputs[key]; ok {
			node.IsFactoryProvided = true
			g.Nodes[key] = node
			continue
		}

		// Tag node: resolve to a concrete item, then continue BFS with the concrete item.
		// The tag node itself is NOT added to g.Nodes; edges are rewritten post-BFS.
		if item.TagRef != "" {
			members, err := s.DB.GetTagMembers(ctx, item.TagRef)
			if err != nil {
				return nil, fmt.Errorf("solver: get tag members for %s: %w", key, err)
			}
			if len(members) == 0 {
				// No members known — treat as raw material input.
				node.IsRawMaterial = true
				g.Nodes[key] = node
				continue
			}
			chosen := members[0]
			if ov, ok := tagOverrides[key]; ok {
				for _, m := range members {
					if m.ModID+":"+m.ItemID == ov {
						chosen = m
						break
					}
				}
			}
			g.TagResolutions[key] = TagResolution{Chosen: chosen, Options: members}
			queue = append(queue, chosen)
			continue
		}

		// Fluid node: only follow production chain if the user explicitly chose a recipe
		// override. Without an override, fluids are raw-material inputs (e.g. water, lava).
		// This prevents auto-following cyclic fluid recipes (e.g. water + X → water).
		if item.IsFluid {
			overrideID, hasOverride := overrides[key]
			if !hasOverride {
				node.IsRawMaterial = true
				g.Nodes[key] = node
				continue
			}
			recipes, err := s.DB.GetRecipesForFluid(ctx, item.ModID, item.ItemID)
			if err != nil {
				return nil, fmt.Errorf("solver: get recipes for fluid %s: %w", key, err)
			}
			recipes = filterByActiveMods(recipes, s.ActiveMods)
			selected := (*RecipeRow)(nil)
			for _, r := range recipes {
				if RecipeOptionKey(r.ID, r.MachineMod, r.MachineID) == overrideID {
					selected = r
					break
				}
			}
			if selected == nil {
				node.IsRawMaterial = true
				g.Nodes[key] = node
				continue
			}
			outputAmount, ok := outputAmountForFluid(selected, item)
			if !ok {
				return nil, fmt.Errorf("solver: recipe %s has no fluid output for %s", selected.ID, key)
			}
			node.RecipeID = selected.ID
			node.MachineMod = selected.MachineMod
			node.MachineID = selected.MachineID
			node.OutputAmount = outputAmount
			appendRecipeEdges(node, selected, &queue)
			g.Nodes[key] = node
			continue
		}

		recipes, err := s.DB.GetRecipesForItem(ctx, item.ModID, item.ItemID)
		if err != nil {
			return nil, fmt.Errorf("solver: get recipes for %s: %w", key, err)
		}
		recipes = filterByActiveMods(recipes, s.ActiveMods)
		if len(recipes) == 0 {
			node.IsRawMaterial = true
			g.Nodes[key] = node
			continue
		}

		selected := recipes[0]
		if overrideID, ok := overrides[key]; ok {
			found := false
			for _, r := range recipes {
				if RecipeOptionKey(r.ID, r.MachineMod, r.MachineID) == overrideID {
					selected = r
					found = true
					break
				}
			}
			if !found {
				node.IsRawMaterial = true
				g.Nodes[key] = node
				continue
			}
		}

		outputAmount, ok := outputAmountForItem(selected, item)
		if !ok {
			return nil, fmt.Errorf("solver: recipe %s has no output for item %s", selected.ID, key)
		}

		node.RecipeID = selected.ID
		node.MachineMod = selected.MachineMod
		node.MachineID = selected.MachineID
		node.OutputAmount = outputAmount

		appendRecipeEdges(node, selected, &queue)

		g.Nodes[key] = node
	}

	// Rewrite any tag-ref edges to the resolved concrete item so the rate vector
	// can flow through normal item keys without special-casing tag aliases.
	for _, node := range g.Nodes {
		for i, edge := range node.Inputs {
			if edge.Item.TagRef == "" {
				continue
			}
			tagKey := edge.Item.Key()
			if res, ok := g.TagResolutions[tagKey]; ok {
				node.Inputs[i].Item = res.Chosen
			}
		}
	}

	return g, nil
}

// appendRecipeEdges populates node.Inputs and node.Outputs from a recipe and extends the BFS queue.
// Fluid edges carry Amount in mB; item edges carry Amount in item units.
func appendRecipeEdges(node *RecipeNode, r *RecipeRow, queue *[]ItemRef) {
	for _, in := range r.ItemInputs {
		var ref ItemRef
		if in.TagName != nil {
			ref = ItemRef{TagRef: *in.TagName}
		} else if in.ItemModID != nil && in.ItemID != nil {
			ref = ItemRef{ModID: *in.ItemModID, ItemID: *in.ItemID}
		} else {
			continue
		}
		node.Inputs = append(node.Inputs, Edge{
			Item:        ref,
			Amount:      NewRational(in.AmountNum, in.AmountDen),
			Probability: NewRational(in.ProbabilityNum, in.ProbabilityDen),
		})
		*queue = append(*queue, ref)
	}
	for _, fi := range r.FluidInputs {
		ref := ItemRef{ModID: fi.FluidModID, ItemID: fi.FluidID, IsFluid: true}
		node.Inputs = append(node.Inputs, Edge{
			Item:        ref,
			Amount:      NewRational(fi.AmountMB, 1),
			Probability: NewRational(fi.ProbabilityNum, fi.ProbabilityDen),
			IsFluid:     true,
			ModID:       fi.FluidModID,
			FluidID:     fi.FluidID,
			AmountMB:    fi.AmountMB,
		})
		*queue = append(*queue, ref)
	}
	for _, out := range r.ItemOutputs {
		if out.ItemModID == nil || out.ItemID == nil {
			continue
		}
		ref := ItemRef{ModID: *out.ItemModID, ItemID: *out.ItemID}
		node.Outputs = append(node.Outputs, Edge{
			Item:        ref,
			Amount:      NewRational(out.AmountNum, out.AmountDen).Mul(NewRational(out.ProbabilityNum, out.ProbabilityDen)),
			Probability: NewRational(out.ProbabilityNum, out.ProbabilityDen),
		})
	}
	for _, fo := range r.FluidOutputs {
		ref := ItemRef{ModID: fo.FluidModID, ItemID: fo.FluidID, IsFluid: true}
		node.Outputs = append(node.Outputs, Edge{
			Item:        ref,
			Amount:      NewRational(fo.AmountMB, 1).Mul(NewRational(fo.ProbabilityNum, fo.ProbabilityDen)),
			Probability: NewRational(fo.ProbabilityNum, fo.ProbabilityDen),
			IsFluid:     true,
			ModID:       fo.FluidModID,
			FluidID:     fo.FluidID,
			AmountMB:    fo.AmountMB,
		})
	}
}

// outputAmountForItem returns the expected net output amount (amount * probability) for the given item.
func outputAmountForItem(r *RecipeRow, item ItemRef) (Rational, bool) {
	for _, out := range r.ItemOutputs {
		if out.ItemModID != nil && out.ItemID != nil && *out.ItemModID == item.ModID && *out.ItemID == item.ItemID {
			base := NewRational(out.AmountNum, out.AmountDen)
			prob := NewRational(out.ProbabilityNum, out.ProbabilityDen)
			return base.Mul(prob), true
		}
	}
	return Rational{}, false
}

// outputAmountForFluid returns the expected net output amount in mB (amount * probability) for the given fluid.
func outputAmountForFluid(r *RecipeRow, item ItemRef) (Rational, bool) {
	for _, out := range r.FluidOutputs {
		if out.FluidModID == item.ModID && out.FluidID == item.ItemID {
			base := NewRational(out.AmountMB, 1)
			prob := NewRational(out.ProbabilityNum, out.ProbabilityDen)
			return base.Mul(prob), true
		}
	}
	return Rational{}, false
}
