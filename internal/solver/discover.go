package solver

import "context"

// RecipeOption is a lightweight recipe summary for the discover UI.
type RecipeOption struct {
	RecipeID   string
	MachineMod string
	MachineID  string
	// Key uniquely identifies this (recipe, machine) choice — use this, not
	// RecipeID, as the override value sent back to the server. RecipeID alone
	// is ambiguous when a recipe is reachable via multiple machine tiers
	// (bronze/steel/electric all implementing the same base recipe).
	Key     string
	Inputs  []string // "item_id", "#tag_name", or "~fluid_id" — complete list;
	Outputs []string // the graph view derives its edges from these, so no cap.
}

// ChainItem is one node in the discovered production chain.
type ChainItem struct {
	Item             ItemRef
	Level            int            // depth from root (root = 0)
	Options          []RecipeOption // empty = raw material
	ChosenRecipeID   string         // selected recipe (first or user override)
	ChosenMachineMod string         // machine actually chosen to run it (may be a tier variant)
	ChosenMachineID  string
	IsStop           bool
	IsRawMaterial    bool
}

// DiscoverResult is the output of Discover.
type DiscoverResult struct {
	Items          []ChainItem
	TagResolutions map[string]TagResolution
}

// Discover runs a BFS from targetItem to collect all items in the production
// chain, along with their available recipes, without computing any rates.
// stopPoints, recipeOverrides, and tagOverrides are the same as in Solve.
func (s *Solver) Discover(
	ctx context.Context,
	targetItem ItemRef,
	stopPoints map[string]bool,
	factoryState FactoryState,
	recipeOverrides map[string]string,
	tagOverrides map[string]string,
) (DiscoverResult, error) {
	res := DiscoverResult{
		TagResolutions: make(map[string]TagResolution),
	}

	type entry struct {
		item  ItemRef
		level int
	}

	queue := []entry{{item: targetItem, level: 0}}
	visited := make(map[string]bool)
	tagRemap := make(map[string]string) // tagKey → concrete item key

	for qi := 0; qi < len(queue); qi++ {
		e := queue[qi]
		item := e.item
		key := item.Key()

		if visited[key] {
			continue
		}
		visited[key] = true

		// Resolve tag to concrete item — tag nodes are not shown in the chain.
		if item.TagRef != "" {
			members, err := s.DB.GetTagMembers(ctx, item.TagRef)
			if err != nil {
				return res, err
			}
			if len(members) == 0 {
				res.Items = append(res.Items, ChainItem{Item: item, Level: e.level, IsRawMaterial: true, IsStop: true})
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
			res.TagResolutions[key] = TagResolution{Chosen: chosen, Options: members}
			tagRemap[key] = chosen.Key()
			if !visited[chosen.Key()] {
				queue = append(queue, entry{item: chosen, level: e.level})
			}
			continue
		}

		ci := ChainItem{Item: item, Level: e.level}

		if stopPoints[key] {
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}
		if _, ok := factoryState.ExistingOutputs[key]; ok {
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}

		var recipes []*RecipeRow
		var err error
		if item.IsFluid {
			recipes, err = s.DB.GetRecipesForFluid(ctx, item.ModID, item.ItemID)
		} else {
			recipes, err = s.DB.GetRecipesForItem(ctx, item.ModID, item.ItemID)
		}
		if err != nil {
			return res, err
		}
		if len(recipes) == 0 {
			ci.IsRawMaterial = true
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}

		for _, r := range recipes {
			opt := RecipeOption{
				RecipeID:   r.ID,
				MachineMod: r.MachineMod,
				MachineID:  r.MachineID,
				Key:        RecipeOptionKey(r.ID, r.MachineMod, r.MachineID),
			}
			for _, in := range r.ItemInputs {
				if in.TagName != nil {
					opt.Inputs = append(opt.Inputs, "#"+*in.TagName)
				} else if in.ItemID != nil {
					opt.Inputs = append(opt.Inputs, *in.ItemID)
				}
			}
			for _, fi := range r.FluidInputs {
				opt.Inputs = append(opt.Inputs, "~"+fi.FluidID)
			}
			for _, out := range r.ItemOutputs {
				if out.ItemID != nil {
					opt.Outputs = append(opt.Outputs, *out.ItemID)
				}
			}
			for _, fo := range r.FluidOutputs {
				opt.Outputs = append(opt.Outputs, "~"+fo.FluidID)
			}
			ci.Options = append(ci.Options, opt)
		}

		// Only follow a recipe if the user explicitly chose one.
		// No override → stop here by default.
		overrideKey, hasOverride := recipeOverrides[key]
		if !hasOverride {
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}

		for _, r := range recipes {
			if RecipeOptionKey(r.ID, r.MachineMod, r.MachineID) == overrideKey {
				ci.ChosenRecipeID = r.ID
				ci.ChosenMachineMod = r.MachineMod
				ci.ChosenMachineID = r.MachineID
				for _, in := range r.ItemInputs {
					var ref ItemRef
					if in.TagName != nil {
						ref = ItemRef{TagRef: *in.TagName}
					} else if in.ItemModID != nil && in.ItemID != nil {
						ref = ItemRef{ModID: *in.ItemModID, ItemID: *in.ItemID}
					} else {
						continue
					}
					if !visited[ref.Key()] {
						queue = append(queue, entry{item: ref, level: e.level + 1})
					}
				}
				for _, fi := range r.FluidInputs {
					ref := ItemRef{ModID: fi.FluidModID, ItemID: fi.FluidID, IsFluid: true}
					if !visited[ref.Key()] {
						queue = append(queue, entry{item: ref, level: e.level + 1})
					}
				}
				break
			}
		}

		res.Items = append(res.Items, ci)
	}

	return res, nil
}
