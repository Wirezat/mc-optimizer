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
	"github.com/Wirezat/production-optimizer/internal/solver"
)

// variantStore is the database surface VariantResolver needs. *db.DB
// implements it; tests substitute a stub.
type variantStore interface {
	GetModPlugin(ctx context.Context, modID string) (*db.ModPlugin, error)
	GetVariants(ctx context.Context, modID, machineID, recipeID, configHash string) ([]plugins.Variant, error)
	PutVariants(ctx context.Context, modID, machineID, recipeID, configHash string, vs []plugins.Variant) error
}

// VariantResolver serves solver.VariantSource from the Postgres cache and
// fills it from the mod's plugin on a miss.
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

// Variants returns the variants for a (machine, recipe) pair under config. A
// mod without a plugin gets the host default; every other failure is returned
// as an error, which the solver turns into a warning and degrades on.
func (r *VariantResolver) Variants(ctx context.Context, machine *solver.MachineSpec, recipe *solver.RecipeRow, config json.RawMessage) ([]plugins.Variant, error) {
	ecosystem := solver.PluginMod(machine)
	// A caller with nothing configured passes nil; normalize it so it cannot share
	// a cache key with a caller that passed "{}".
	config = normalizeConfig(config)

	plug, err := r.store.GetModPlugin(ctx, ecosystem)
	if err != nil {
		// Only "this mod ships no plugin" means the host default applies; any other
		// error surfaces.
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
		// The variants are already computed and correct; the cache is an
		// optimization. Failing here would degrade the whole mod to the host
		// default through the solver's warning path.
		GoLog.Warnf("service: cache variants for %s:%s recipe %s: %v",
			machine.ModID, machine.MachineID, recipe.ID, err)
	}
	return vs, nil
}

// program returns the compiled plugin for modID, compiling it on a registry
// miss. Compiling can take seconds and can fail on a broken plugin.
func (r *VariantResolver) program(modID string, plug *db.ModPlugin) (*plugins.Program, error) {
	if prog, ok := r.registry.Get(modID, plug.Version); ok {
		return prog, nil
	}
	if err := r.registry.Put(modID, plug.Version, plug.Source); err != nil {
		return nil, fmt.Errorf("service: compile plugin %s: %w", modID, err)
	}
	prog, ok := r.registry.Get(modID, plug.Version)
	if !ok {
		// A concurrent Put for another version of the same mod replaced the
		// entry between the two calls. Reporting it beats dereferencing nil.
		return nil, fmt.Errorf("service: plugin %s version %s is no longer registered", modID, plug.Version)
	}
	return prog, nil
}

// normalizeConfig treats a nil or empty config as the empty object, the same
// convention db.VariantCacheHash applies when hashing.
func normalizeConfig(cfg json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(cfg)) == 0 {
		return json.RawMessage(`{}`)
	}
	return cfg
}

// recipeOutputs maps a recipe's catalog outputs onto the plugin wire type,
// amount and probability as separate exact fractions. Refs come from
// solver.ItemRef.Key(), never from string concatenation.
func recipeOutputs(recipe *solver.RecipeRow) []plugins.Output {
	out := make([]plugins.Output, 0, len(recipe.ItemOutputs)+len(recipe.FluidOutputs))
	for _, o := range recipe.ItemOutputs {
		if o.ItemModID == nil || o.ItemID == nil {
			continue
		}
		ref := solver.ItemRef{ModID: *o.ItemModID, ItemID: *o.ItemID}
		out = append(out, plugins.Output{
			Ref:         ref.Key(),
			Amount:      plugins.Rational{Num: o.AmountNum, Den: o.AmountDen},
			Probability: plugins.Rational{Num: o.ProbabilityNum, Den: o.ProbabilityDen},
		})
	}
	for _, f := range recipe.FluidOutputs {
		ref := solver.ItemRef{ModID: f.FluidModID, ItemID: f.FluidID, IsFluid: true}
		out = append(out, plugins.Output{
			Ref:         ref.Key(),
			Amount:      plugins.Rational{Num: f.AmountMB, Den: 1},
			Probability: plugins.Rational{Num: f.ProbabilityNum, Den: f.ProbabilityDen},
		})
	}
	return out
}

// recipeInputs maps a recipe's catalog inputs onto the plugin wire type, using
// the same ref format as recipeOutputs. A tag input carries its tag key.
func recipeInputs(recipe *solver.RecipeRow) []plugins.Output {
	in := make([]plugins.Output, 0, len(recipe.ItemInputs)+len(recipe.FluidInputs))
	for _, i := range recipe.ItemInputs {
		var ref solver.ItemRef
		switch {
		case i.TagName != nil:
			ref = solver.ItemRef{TagRef: *i.TagName}
		case i.ItemModID != nil && i.ItemID != nil:
			ref = solver.ItemRef{ModID: *i.ItemModID, ItemID: *i.ItemID}
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
		ref := solver.ItemRef{ModID: f.FluidModID, ItemID: f.FluidID, IsFluid: true}
		in = append(in, plugins.Output{
			Ref:         ref.Key(),
			Amount:      plugins.Rational{Num: f.AmountMB, Den: 1},
			Probability: plugins.Rational{Num: f.ProbabilityNum, Den: f.ProbabilityDen},
		})
	}
	return in
}
