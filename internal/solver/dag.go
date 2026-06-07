package solver

import "fmt"

// RateVector holds the computed rates for recipes and items.
type RateVector struct {
	RecipeRates map[string]Rational // recipe_uuid → Executions/Tick
	ItemRates   map[string]Rational // ItemRef.Key() → Net-Flow/Tick
}

// newRateVector creates an empty RateVector with initialized maps.
func newRateVector() RateVector {
	return RateVector{
		RecipeRates: make(map[string]Rational),
		ItemRates:   make(map[string]Rational),
	}
}

// DetectCycles returns true if the recipe graph contains a cycle.
func DetectCycles(g *RecipeGraph) bool {
	_, err := TopologicalSort(g)
	return err != nil
}

// TopologicalSort orders the recipe graph nodes via Kahn's algorithm, returning an error if a cycle is detected.
func TopologicalSort(g *RecipeGraph) ([]*RecipeNode, error) {
	n := len(g.Nodes)
	inDegree := make(map[string]int, n)
	dependents := make(map[string][]string, n)

	for key, node := range g.Nodes {
		if _, exists := inDegree[key]; !exists {
			inDegree[key] = 0
		}
		for _, edge := range node.Inputs {
			depKey := edge.Item.Key()
			dependents[depKey] = append(dependents[depKey], key)
			inDegree[key]++
		}
	}

	queue := make([]string, 0, n)
	for key, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, key)
		}
	}

	order := make([]*RecipeNode, 0, n)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		order = append(order, g.Nodes[key])
		for _, dep := range dependents[key] {
			if inDegree[dep]--; inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(order) != n {
		return nil, fmt.Errorf("solver: cycle detected")
	}
	return order, nil
}

// SolveDAG computes recipe and item rates for a target output rate by walking the topological order in reverse.
func SolveDAG(g *RecipeGraph, targetRatePerTick Rational) (RateVector, error) {
	topoOrder, err := TopologicalSort(g)
	if err != nil {
		return RateVector{}, err
	}

	n := len(topoOrder)
	rv := newRateVector()
	rv.ItemRates[g.Root.Key()] = targetRatePerTick

	for i := n - 1; i >= 0; i-- {
		node := topoOrder[i]
		key := node.Item.Key()

		itemRate, ok := rv.ItemRates[key]
		if !ok || node.RecipeID == "" {
			continue
		}
		if node.OutputAmount.IsZero() {
			return RateVector{}, fmt.Errorf("solver: recipe %s: zero output for %s", node.RecipeID, key)
		}

		recipeRate := itemRate.Div(node.OutputAmount)
		rv.RecipeRates[node.RecipeID] = recipeRate
		rv.ItemRates[key] = itemRate

		for _, edge := range node.Inputs {
			edgeKey := edge.Item.Key()
			needed := recipeRate.Mul(edge.Amount).Mul(edge.Probability)
			if existing, ok := rv.ItemRates[edgeKey]; ok {
				rv.ItemRates[edgeKey] = existing.Add(needed)
			} else {
				rv.ItemRates[edgeKey] = needed
			}
		}
	}

	return rv, nil
}
