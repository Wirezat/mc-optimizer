package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// variantStore is the database surface VariantResolver needs.
type variantStore interface {
	GetModPlugin(ctx context.Context, modID string) (*db.ModPlugin, error)
	GetVariants(ctx context.Context, modID, machineID, recipeID, configHash string) ([]plugins.Variant, error)
	PutVariants(ctx context.Context, modID, machineID, recipeID, configHash string, vs []plugins.Variant) error
}

// VariantResolver serves solver.VariantSource from the Postgres cache and fills it from the
// mod's plugin on a miss.
type VariantResolver struct {
	store    variantStore
	registry *plugins.Registry
}

// NewVariantResolver connects the cache and the plugin registry.
func NewVariantResolver(database *db.DB, registry *plugins.Registry) *VariantResolver {
	return newVariantResolver(database, registry)
}

func newVariantResolver(store variantStore, registry *plugins.Registry) *VariantResolver {
	return &VariantResolver{store: store, registry: registry}
}

// Variants returns the variants for a (machine, recipe) pair under config.
func (r *VariantResolver) Variants(ctx context.Context, machine *solver.MachineSpec, recipe *solver.RecipeRow, config json.RawMessage) ([]plugins.Variant, error) {
	ecosystem := solver.PluginMod(machine)
	config = normalizeConfig(config)

	plug, err := r.store.GetModPlugin(ctx, ecosystem)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return []plugins.Variant{solver.DefaultVariant(recipe)}, nil
		}
		return nil, err
	}

	hash := db.VariantCacheHash(plug.Version, machine.ModData, recipe.ModData, config)
	cached, err := r.store.GetVariants(ctx, machine.ModID, machine.MachineID, recipe.ID, hash)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}

	prog, err := r.program(ecosystem, plug)
	if err != nil {
		return nil, err
	}

	vs, err := prog.Evaluate(ctx, plugins.EvalContext{
		Machine: plugins.EvalMachine{ModID: machine.ModID, MachineID: machine.MachineID, Data: machine.ModData},
		Recipe: plugins.EvalRecipe{
			ID:            recipe.ID,
			MachineMod:    recipe.MachineMod,
			MachineID:     recipe.MachineID,
			DurationTicks: int64(recipe.DurationTicks),
			Inputs:        recipeInputs(recipe),
			Outputs:       recipeOutputs(recipe),
			Data:          recipe.ModData,
		},
		Config: config,
	})
	if err != nil {
		return nil, err
	}

	if err := r.store.PutVariants(ctx, machine.ModID, machine.MachineID, recipe.ID, hash, vs); err != nil {
		GoLog.Warnf("service: cache variants for %s:%s recipe %s: %v",
			machine.ModID, machine.MachineID, recipe.ID, err)
	}
	return vs, nil
}

// program returns the compiled plugin for modID, compiling it on a registry miss.
func (r *VariantResolver) program(modID string, plug *db.ModPlugin) (*plugins.Program, error) {
	if prog, ok := r.registry.Get(modID, plug.Version); ok {
		return prog, nil
	}
	if err := r.registry.Put(modID, plug.Version, plug.Source); err != nil {
		return nil, fmt.Errorf("service: compile plugin %s: %w", modID, err)
	}
	prog, ok := r.registry.Get(modID, plug.Version)
	if !ok {
		return nil, fmt.Errorf("service: plugin %s version %s is no longer registered", modID, plug.Version)
	}
	return prog, nil
}

// normalizeConfig returns the empty object for a nil or empty config.
func normalizeConfig(cfg json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(cfg)) == 0 {
		return json.RawMessage(`{}`)
	}
	return cfg
}

// recipeOutputs maps a recipe's catalog outputs onto the plugin wire type.
func recipeOutputs(recipe *solver.RecipeRow) []plugins.Output {
	out := make([]plugins.Output, 0, len(recipe.ItemOutputs)+len(recipe.FluidOutputs))
	for _, o := range recipe.ItemOutputs {
		if o.ItemModID == nil || o.ItemID == nil {
			continue
		}
		ref := solver.ResourceRef{ModID: *o.ItemModID, ID: *o.ItemID}
		out = append(out, plugins.Output{
			Ref:         ref.Key(),
			Amount:      plugins.Rational{Num: o.AmountNum, Den: o.AmountDen},
			Probability: plugins.Rational{Num: o.ProbabilityNum, Den: o.ProbabilityDen},
		})
	}
	for _, f := range recipe.FluidOutputs {
		ref := solver.ResourceRef{ModID: f.FluidModID, ID: f.FluidID, Kind: resource.KindFluid}
		out = append(out, plugins.Output{
			Ref:         ref.Key(),
			Amount:      plugins.Rational{Num: f.AmountMB, Den: 1},
			Probability: plugins.Rational{Num: f.ProbabilityNum, Den: f.ProbabilityDen},
		})
	}
	return out
}

// recipeInputs maps a recipe's catalog inputs onto the plugin wire type.
func recipeInputs(recipe *solver.RecipeRow) []plugins.Output {
	in := make([]plugins.Output, 0, len(recipe.ItemInputs)+len(recipe.FluidInputs))
	for _, i := range recipe.ItemInputs {
		var ref solver.ResourceRef
		switch {
		case i.TagName != nil:
			ref = solver.ResourceRef{TagRef: *i.TagName}
		case i.ItemModID != nil && i.ItemID != nil:
			ref = solver.ResourceRef{ModID: *i.ItemModID, ID: *i.ItemID}
		default:
			continue
		}
		in = append(in, plugins.Output{
			Ref:         ref.Key(),
			Amount:      plugins.Rational{Num: i.AmountNum, Den: i.AmountDen},
			Probability: plugins.Rational{Num: i.ProbabilityNum, Den: i.ProbabilityDen},
		})
	}
	for _, f := range recipe.FluidInputs {
		ref := f.Ref()
		in = append(in, plugins.Output{
			Ref:         ref.Key(),
			Amount:      plugins.Rational{Num: f.AmountMB, Den: 1},
			Probability: plugins.Rational{Num: f.ProbabilityNum, Den: f.ProbabilityDen},
		})
	}
	return in
}
