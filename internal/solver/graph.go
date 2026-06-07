package solver

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/model"
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

// RecipeGraph maps item keys to RecipeNode and stores the root item.
type RecipeGraph struct {
	Nodes map[string]*RecipeNode
	Root  ItemRef
}

// BuildRecipeGraph constructs a directed recipe graph starting from the root item.
// It stops at stop points, factory-provided items, or raw materials (no recipe).
func (s *Solver) BuildRecipeGraph(
	ctx context.Context,
	root ItemRef,
	stopPoints map[string]bool,
	factory FactoryState,
	overrides map[string]string,
) (*RecipeGraph, error) {
	g := &RecipeGraph{
		Nodes: make(map[string]*RecipeNode),
		Root:  root,
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

		recipes, err := s.DB.GetRecipesForItem(ctx, item.ModID, item.ItemID)
		if err != nil {
			return nil, fmt.Errorf("solver: get recipes for %s: %w", key, err)
		}
		if len(recipes) == 0 {
			node.IsRawMaterial = true
			g.Nodes[key] = node
			continue
		}

		selected := recipes[0]
		if overrideID, ok := overrides[key]; ok {
			for _, r := range recipes {
				if r.ID == overrideID {
					selected = r
					break
				}
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

		for _, in := range selected.ItemInputs {
			if in.ItemModID == nil || in.ItemID == nil {
				continue
			}
			ref := ItemRef{ModID: *in.ItemModID, ItemID: *in.ItemID}
			edge := Edge{
				Item:        ref,
				Amount:      NewRational(in.AmountNum, in.AmountDen),
				Probability: NewRational(in.ProbabilityNum, in.ProbabilityDen),
			}
			node.Inputs = append(node.Inputs, edge)
			queue = append(queue, ref)
		}

		for _, fi := range selected.FluidInputs {
			node.Inputs = append(node.Inputs, Edge{
				ModID:       fi.FluidModID,
				FluidID:     fi.FluidID,
				AmountMB:    fi.AmountMB,
				Probability: NewRational(fi.ProbabilityNum, fi.ProbabilityDen),
				IsFluid:     true,
			})
		}

		for _, fo := range selected.FluidOutputs {
			node.Outputs = append(node.Outputs, Edge{
				ModID:       fo.FluidModID,
				FluidID:     fo.FluidID,
				AmountMB:    fo.AmountMB,
				Probability: NewRational(fo.ProbabilityNum, fo.ProbabilityDen),
				IsFluid:     true,
			})
		}

		g.Nodes[key] = node
	}
	return g, nil
}

// outputAmountForItem returns the expected net output amount (amount * probability) for the given item.
func outputAmountForItem(r *model.RecipeRow, item ItemRef) (Rational, bool) {
	for _, out := range r.ItemOutputs {
		if out.ItemModID == item.ModID && out.ItemID == item.ItemID {
			base := NewRational(out.AmountNum, out.AmountDen)
			prob := NewRational(out.ProbabilityNum, out.ProbabilityDen)
			return base.Mul(prob), true
		}
	}
	return Rational{}, false
}

// AllNodes returns all nodes in the graph as a slice.
func (g *RecipeGraph) AllNodes() []*RecipeNode {
	nodes := make([]*RecipeNode, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// ActiveRecipeNodes returns only the nodes that have a non-empty RecipeID.
func (g *RecipeGraph) ActiveRecipeNodes() []*RecipeNode {
	var nodes []*RecipeNode
	for _, n := range g.Nodes {
		if n.RecipeID != "" {
			nodes = append(nodes, n)
		}
	}
	return nodes
}
