package solver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// Edge represents a directed connection between a recipe node and an item or fluid.
type Edge struct {
	Item        resource.Ref
	Amount      resource.Rational
	Probability resource.Rational
	Consumed    bool
}

// RecipeNode represents a single item in the recipe graph, optionally bound to a specific recipe.
type RecipeNode struct {
	Item              resource.Ref
	RecipeID          string
	MachineMod        string
	MachineID         string
	Inputs            []Edge
	Outputs           []Edge
	OutputAmount      resource.Rational
	IsStopPoint       bool
	IsRawMaterial     bool
	IsFactoryProvided bool
	Siblings          []string
}

// RateKey identifies this node's (recipe, machine) pair for rate-tracking purposes.
func (n *RecipeNode) RateKey() string {
	return RecipeOptionKey(n.RecipeID, n.MachineMod, n.MachineID)
}

// RecipeOptionKey uniquely identifies a (recipe, machine) choice.
func RecipeOptionKey(recipeID, machineMod, machineID string) string {
	return recipeID + "@" + machineMod + ":" + machineID
}

// SplitRecipeOverride splits an override value into recipe and optional machine.
func SplitRecipeOverride(value string) (recipeID, machineMod, machineID string, hasMachine bool) {
	if id, mod, machine, ok := ParseRecipeOptionKey(value); ok {
		return id, mod, machine, true
	}
	return value, "", "", false
}

// selectsRecipe reports whether an override value picks this recipe row.
func selectsRecipe(override string, r *RecipeRow) bool {
	id, mod, machine, hasMachine := SplitRecipeOverride(override)
	if r.ID != id {
		return false
	}
	return !hasMachine || (r.MachineMod == mod && r.MachineID == machine)
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
	Root           resource.Ref
	TagResolutions map[string]TagResolution // tagKey → resolved item + options
}

// BuildRecipeGraph constructs a directed recipe graph starting from the root item.
func (s *Solver) BuildRecipeGraph(
	ctx context.Context,
	root resource.Ref,
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
	queue := []resource.Ref{root}
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

		if item.TagRef != "" {
			members, err := s.DB.GetTagMembers(ctx, item)
			if err != nil {
				return nil, fmt.Errorf("solver: get tag members for %s: %w", key, err)
			}
			if len(members) == 0 {
				node.IsRawMaterial = true
				g.Nodes[key] = node
				continue
			}
			chosen := members[0]
			if ov, ok := tagOverrides[key]; ok {
				for _, m := range members {
					if m.ModID+":"+m.ID == ov {
						chosen = m
						break
					}
				}
			}
			g.TagResolutions[key] = TagResolution{Chosen: chosen, Options: members}
			queue = append(queue, chosen)
			continue
		}

		rule := resource.Expand(item.Kind)
		overrideID, hasOverride := overrides[key]
		if rule == resource.ExpandNever || (rule == resource.ExpandOnChoice && !hasOverride) {
			node.IsRawMaterial = true
			g.Nodes[key] = node
			continue
		}
		recipes, err := s.DB.GetRecipesFor(ctx, item)
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
		if hasOverride {
			found := false
			for _, r := range recipes {
				if selectsRecipe(overrideID, r) {
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

		outputAmount, ok := outputAmountFor(selected, item)
		if !ok {
			return nil, fmt.Errorf("solver: recipe %s has no output for %s", selected.ID, key)
		}

		node.RecipeID = selected.ID
		node.MachineMod = selected.MachineMod
		node.MachineID = selected.MachineID
		node.OutputAmount = outputAmount
		if rule == resource.ExpandAlways {
			node.Siblings = siblingIDs(selected, recipes)
		}

		appendRecipeEdges(node, selected, &queue)

		g.Nodes[key] = node
	}

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

// siblingIDs returns the ids of the rows whose I/O matches selected's, itself included.
func siblingIDs(selected *RecipeRow, rows []*RecipeRow) []string {
	want := ioSignature(selected)
	out := []string{selected.ID}
	seen := map[string]bool{selected.ID: true}
	for _, r := range rows {
		if seen[r.ID] || ioSignature(r) != want {
			continue
		}
		seen[r.ID] = true
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

// ioSignature is a recipe's I/O in canonical form, inputs and outputs apart.
func ioSignature(r *RecipeRow) string {
	var b strings.Builder
	b.WriteString("in:")
	writeIOSignature(&b, r.Inputs)
	b.WriteString("|out:")
	writeIOSignature(&b, r.Outputs)
	return b.String()
}

func writeIOSignature(b *strings.Builder, ios []resource.IO) {
	all := make([]string, 0, len(ios))
	for _, io := range ios {
		all = append(all, fmt.Sprintf("%s=%s*%s/%t", io.Ref.Key(), io.Amount, io.Prob, !io.Consumed))
	}
	sort.Strings(all)
	for _, p := range all {
		b.WriteString(p)
		b.WriteByte(';')
	}
}

// appendRecipeEdges populates node.Inputs and node.Outputs from a recipe and extends the BFS queue.
func appendRecipeEdges(node *RecipeNode, r *RecipeRow, queue *[]resource.Ref) {
	for _, in := range r.Inputs {
		node.Inputs = append(node.Inputs, Edge{Item: in.Ref, Amount: in.Amount, Probability: in.Prob, Consumed: in.Consumed})
		*queue = append(*queue, in.Ref)
	}
	for _, out := range r.Outputs {
		if out.Ref.TagRef != "" {
			continue
		}
		node.Outputs = append(node.Outputs, Edge{Item: out.Ref, Amount: out.Amount.Mul(out.Prob), Probability: out.Prob, Consumed: true})
	}
}

// outputAmountFor returns how much of ref one craft of r yields (amount × probability), and whether r yields it.
func outputAmountFor(r *RecipeRow, ref resource.Ref) (resource.Rational, bool) {
	for _, out := range r.Outputs {
		if out.Ref.Key() == ref.Key() {
			return out.Amount.Mul(out.Prob), true
		}
	}
	return resource.Rational{}, false
}
