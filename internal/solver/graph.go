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
	Item        ResourceRef
	Amount      Rational
	Probability Rational
}

// RecipeNode represents a single item in the recipe graph, optionally bound to a specific recipe.
type RecipeNode struct {
	Item              ResourceRef
	RecipeID          string
	MachineMod        string
	MachineID         string
	Inputs            []Edge
	Outputs           []Edge
	OutputAmount      Rational
	IsStopPoint       bool
	IsRawMaterial     bool
	IsFactoryProvided bool
	// Siblings are the recipe ids with I/O identical to RecipeID's, itself included.
	Siblings []string
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
	Root           ResourceRef
	TagResolutions map[string]TagResolution // tagKey → resolved item + options
}

// BuildRecipeGraph constructs a directed recipe graph starting from the root item.
func (s *Solver) BuildRecipeGraph(
	ctx context.Context,
	root ResourceRef,
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
	queue := []ResourceRef{root}
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

		// Tag node: resolve to a member, then continue with that member.
		if item.TagRef != "" {
			members, err := s.DB.GetTagMembers(ctx, item)
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
		recipes, err := s.recipesFor(ctx, item)
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
	writeIOSignature(&b, itemSignatures(r.ItemInputs), fluidSignatures(r.FluidInputs))
	b.WriteString("|out:")
	writeIOSignature(&b, itemSignatures(r.ItemOutputs), fluidSignatures(r.FluidOutputs))
	return b.String()
}

func writeIOSignature(b *strings.Builder, parts ...[]string) {
	all := make([]string, 0, 8)
	for _, p := range parts {
		all = append(all, p...)
	}
	sort.Strings(all)
	for _, p := range all {
		b.WriteString(p)
		b.WriteByte(';')
	}
}

func itemSignatures(ios []RecipeRowItemIO) []string {
	out := make([]string, 0, len(ios))
	for _, io := range ios {
		var ref ResourceRef
		switch {
		case io.TagName != nil:
			ref = ResourceRef{TagRef: *io.TagName}
		case io.ItemModID != nil && io.ItemID != nil:
			ref = ResourceRef{ModID: *io.ItemModID, ID: *io.ItemID}
		default:
			continue
		}
		out = append(out, fmt.Sprintf("%s=%s*%s/%t", ref.Key(),
			NewRational(io.AmountNum, io.AmountDen), NewRational(io.ProbabilityNum, io.ProbabilityDen),
			io.NonConsuming))
	}
	return out
}

func fluidSignatures(ios []RecipeRowFluidIO) []string {
	out := make([]string, 0, len(ios))
	for _, io := range ios {
		ref := io.Ref()
		out = append(out, fmt.Sprintf("%s=%s*%s", ref.Key(),
			NewRational(io.AmountMB, 1), NewRational(io.ProbabilityNum, io.ProbabilityDen)))
	}
	return out
}

// appendRecipeEdges populates node.Inputs and node.Outputs from a recipe and extends the BFS queue.
func appendRecipeEdges(node *RecipeNode, r *RecipeRow, queue *[]ResourceRef) {
	for _, in := range r.ItemInputs {
		var ref ResourceRef
		if in.TagName != nil {
			ref = ResourceRef{TagRef: *in.TagName}
		} else if in.ItemModID != nil && in.ItemID != nil {
			ref = ResourceRef{ModID: *in.ItemModID, ID: *in.ItemID}
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
		ref := fi.Ref()
		node.Inputs = append(node.Inputs, Edge{
			Item:        ref,
			Amount:      NewRational(fi.AmountMB, 1),
			Probability: NewRational(fi.ProbabilityNum, fi.ProbabilityDen),
		})
		*queue = append(*queue, ref)
	}
	for _, out := range r.ItemOutputs {
		if out.ItemModID == nil || out.ItemID == nil {
			continue
		}
		ref := ResourceRef{ModID: *out.ItemModID, ID: *out.ItemID}
		node.Outputs = append(node.Outputs, Edge{
			Item:        ref,
			Amount:      NewRational(out.AmountNum, out.AmountDen).Mul(NewRational(out.ProbabilityNum, out.ProbabilityDen)),
			Probability: NewRational(out.ProbabilityNum, out.ProbabilityDen),
		})
	}
	for _, fo := range r.FluidOutputs {
		ref := ResourceRef{ModID: fo.FluidModID, ID: fo.FluidID, Kind: resource.KindFluid}
		node.Outputs = append(node.Outputs, Edge{
			Item:        ref,
			Amount:      NewRational(fo.AmountMB, 1).Mul(NewRational(fo.ProbabilityNum, fo.ProbabilityDen)),
			Probability: NewRational(fo.ProbabilityNum, fo.ProbabilityDen),
		})
	}
}

// outputAmountForItem returns the expected net output amount (amount * probability) for the given item.
func (s *Solver) recipesFor(ctx context.Context, ref ResourceRef) ([]*RecipeRow, error) {
	switch ref.Kind.Or() {
	case resource.KindItem:
		return s.DB.GetRecipesForItem(ctx, ref.ModID, ref.ID)
	case resource.KindFluid:
		return s.DB.GetRecipesForFluid(ctx, ref.ModID, ref.ID)
	}
	return nil, fmt.Errorf("solver: no recipe lookup for kind %q", ref.Kind)
}

// outputAmountFor returns how much of ref one craft of r yields, and whether r yields it.
func outputAmountFor(r *RecipeRow, ref ResourceRef) (Rational, bool) {
	if ref.Kind.Or() == resource.KindFluid {
		return outputAmountForFluid(r, ref)
	}
	return outputAmountForItem(r, ref)
}

func outputAmountForItem(r *RecipeRow, item ResourceRef) (Rational, bool) {
	for _, out := range r.ItemOutputs {
		if out.ItemModID != nil && out.ItemID != nil && *out.ItemModID == item.ModID && *out.ItemID == item.ID {
			base := NewRational(out.AmountNum, out.AmountDen)
			prob := NewRational(out.ProbabilityNum, out.ProbabilityDen)
			return base.Mul(prob), true
		}
	}
	return Rational{}, false
}

// outputAmountForFluid returns the expected net output amount in mB (amount * probability) for the given fluid.
func outputAmountForFluid(r *RecipeRow, item ResourceRef) (Rational, bool) {
	for _, out := range r.FluidOutputs {
		if out.FluidModID == item.ModID && out.FluidID == item.ID {
			base := NewRational(out.AmountMB, 1)
			prob := NewRational(out.ProbabilityNum, out.ProbabilityDen)
			return base.Mul(prob), true
		}
	}
	return Rational{}, false
}
