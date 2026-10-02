package solver

import (
	"context"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// RecipeOption is a lightweight recipe summary for the discover UI.
type RecipeOption struct {
	RecipeID   string   `json:"recipe_id"`
	MachineMod string   `json:"machine_mod"`
	MachineID  string   `json:"machine_id"`
	Key        string   `json:"key"`
	IOKey      string   `json:"io_key"`  // options with identical I/O; the solver picks between them
	Inputs     []string `json:"inputs"`  // "item_id", "#tag_name", or "~fluid_id" — complete list;
	Outputs    []string `json:"outputs"` // the graph view derives its edges from these, so no cap.
	// recipe the others speed up (e.g.
	DurationTicks int `json:"duration_ticks"`
}

// ChainItem is one node in the discovered production chain.
type ChainItem struct {
	Item             resource.Ref   `json:"ref"`
	Level            int            `json:"level"`              // depth from root (root = 0)
	Options          []RecipeOption `json:"options"`            // empty = raw material
	ChosenRecipeID   string         `json:"chosen_recipe_id"`   // selected recipe (first or user override)
	ChosenMachineMod string         `json:"chosen_machine_mod"` // machine actually chosen to run it (may be a tier variant)
	ChosenMachineID  string         `json:"chosen_machine_id"`
	IsStop           bool           `json:"is_stop"`
	IsRawMaterial    bool           `json:"is_raw_material"`
	ModRestricted    bool           `json:"mod_restricted"`
	// Empty for chain items that a recipe names directly.
	ResolvedTag string `json:"resolved_tag"`
}

// DiscoverResult is the output of Discover.
type DiscoverResult struct {
	Items          []ChainItem
	TagResolutions map[string]TagResolution
}

// Discover runs a BFS from targetItem to collect all items in the production chain, along
// with their available recipes, without computing any rates.
func (s *Solver) Discover(
	ctx context.Context,
	targetItem resource.Ref,
	stopPoints map[string]bool,
	factoryState FactoryState,
	recipeOverrides map[string]string,
	tagOverrides map[string]string,
) (DiscoverResult, error) {
	res := DiscoverResult{
		TagResolutions: make(map[string]TagResolution),
	}

	type entry struct {
		item        resource.Ref
		level       int
		resolvedTag string // set when item was queued by resolving this tag key
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
			members, err := s.DB.GetTagMembers(ctx, item)
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
					if m.ModID+":"+m.ID == ov {
						chosen = m
						break
					}
				}
			}
			res.TagResolutions[key] = TagResolution{Chosen: chosen, Options: members}
			tagRemap[key] = chosen.Key()
			if !visited[chosen.Key()] {
				queue = append(queue, entry{item: chosen, level: e.level, resolvedTag: key})
			}
			continue
		}

		ci := ChainItem{Item: item, Level: e.level, ResolvedTag: e.resolvedTag}

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

		if resource.Expand(item.Kind) == resource.ExpandNever {
			ci.IsRawMaterial = true
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}
		recipes, err := s.DB.GetRecipesFor(ctx, item)
		if err != nil {
			return res, err
		}
		filtered := filterByActiveMods(recipes, s.ActiveMods)
		if len(recipes) > 0 && len(filtered) == 0 {
			ci.ModRestricted = true
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}
		recipes = filtered
		if len(recipes) == 0 {
			ci.IsRawMaterial = true
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}

		for _, r := range recipes {
			opt := RecipeOption{
				RecipeID:      r.ID,
				MachineMod:    r.MachineMod,
				MachineID:     r.MachineID,
				Key:           RecipeOptionKey(r.ID, r.MachineMod, r.MachineID),
				IOKey:         ioSignature(r),
				DurationTicks: r.DurationTicks,
			}
			for _, in := range r.Inputs {
				opt.Inputs = append(opt.Inputs, in.Ref.Key())
			}
			for _, out := range r.Outputs {
				opt.Outputs = append(opt.Outputs, out.Ref.Key())
			}
			ci.Options = append(ci.Options, opt)
		}

		// Only follow a recipe if the user explicitly chose one. No override → stop here by
		// default.
		overrideKey, hasOverride := recipeOverrides[key]
		if !hasOverride {
			ci.IsStop = true
			res.Items = append(res.Items, ci)
			continue
		}

		for _, r := range recipes {
			if selectsRecipe(overrideKey, r) {
				ci.ChosenRecipeID = r.ID
				ci.ChosenMachineMod = r.MachineMod
				ci.ChosenMachineID = r.MachineID
				for _, in := range r.Inputs {
					if !visited[in.Ref.Key()] {
						queue = append(queue, entry{item: in.Ref, level: e.level + 1})
					}
				}
				break
			}
		}

		res.Items = append(res.Items, ci)
	}

	return res, nil
}
