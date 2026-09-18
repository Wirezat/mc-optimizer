package solver

import "fmt"

// RateVector holds the computed rates for recipes and items.
type RateVector struct {
	RecipeRates map[string]Rational // RecipeOptionKey(recipe, machine) → Executions/Tick
	ItemRates   map[string]Rational // ItemRef.Key() → Net-Flow/Tick
}

// newRateVector creates an empty RateVector with initialized maps.
func newRateVector() RateVector {
	return RateVector{
		RecipeRates: make(map[string]Rational),
		ItemRates:   make(map[string]Rational),
	}
}

// DetectCycles returns ("", false) when acyclic, or (message with cycle nodes, true) when
// cyclic.
func DetectCycles(g *RecipeGraph) (string, bool) {
	_, err := TopologicalSort(g)
	if err != nil {
		return err.Error(), true
	}
	return "", false
}

// CyclicNodeKeys returns the keys of all nodes involved in cycles, or nil if the graph is
// acyclic.
func CyclicNodeKeys(g *RecipeGraph) []string {
	n := len(g.Nodes)
	inDegree := make(map[string]int, n)
	dependents := make(map[string][]string, n)

	for key, node := range g.Nodes {
		if _, exists := inDegree[key]; !exists {
			inDegree[key] = 0
		}
		for _, edge := range node.Inputs {
			depKey := edge.Item.Key()
			if _, exists := g.Nodes[depKey]; !exists {
				continue // edge points outside the graph (unresolved tag, etc.)
			}
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
	processed := 0
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		processed++
		for _, dep := range dependents[key] {
			if inDegree[dep]--; inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}
	if processed == n {
		return nil
	}
	var keys []string
	for key, deg := range inDegree {
		if deg > 0 {
			keys = append(keys, key)
		}
	}
	return keys
}

// TopologicalSort orders the recipe graph nodes via Kahn's algorithm, returning an error if
// a cycle is detected.
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
			if _, exists := g.Nodes[depKey]; !exists {
				continue // edge points outside the graph
			}
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
		var cycleNodes []string
		for key, deg := range inDegree {
			if deg > 0 {
				cycleNodes = append(cycleNodes, key)
			}
		}
		return nil, fmt.Errorf("solver: cycle detected — nodes in cycle: %v", cycleNodes)
	}
	return order, nil
}

// SolveDAG computes recipe and item rates for a target output rate by walking the
// topological order in reverse.
func SolveDAG(g *RecipeGraph, targetRatePerTick Rational) (RateVector, error) {
	topoOrder, err := TopologicalSort(g)
	if err != nil {
		return RateVector{}, err
	}

	n := len(topoOrder)
	rv := newRateVector()
	rv.ItemRates[g.Root.Key()] = targetRatePerTick

	// Pass 1: propagate demands from root to leaves (reverse topological order).
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
		rv.RecipeRates[node.RateKey()] = recipeRate
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
