package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ItemModID/ItemID: item identity (mutually exclusive with TagID)
// TagID: tag-based input instead of specific item
// AmountNum/Den: rational quantity (num/den)
// ProbabilityNum/Den: chance this input is consumed per craft (rational)
type RecipeItemInput struct {
	ItemModID      *string // nil if tag-based
	ItemID         *string
	TagID          *string // nil if item-based
	AmountNum      int64
	AmountDen      int64
	ProbabilityNum int64
	ProbabilityDen int64
}

// fixed item output — no tag ambiguity possible on outputs
type RecipeItemOutput struct {
	ItemModID      string
	ItemID         string
	AmountNum      int64
	AmountDen      int64
	ProbabilityNum int64
	ProbabilityDen int64
}

// fluid quantity in millibuckets
type RecipeFluidInput struct {
	FluidModID     string
	FluidID        string
	AmountMB       int64
	ProbabilityNum int64
	ProbabilityDen int64
}

type RecipeFluidOutput struct {
	FluidModID     string
	FluidID        string
	AmountMB       int64
	ProbabilityNum int64
	ProbabilityDen int64
}

// full recipe with all inputs/outputs hydrated
type RecipeWithIO struct {
	ID            string
	MachineMod    string
	MachineID     string
	DurationTicks int
	TotalEU       int64
	Priority      int // higher = preferred when multiple recipes produce same item
	ItemInputs    []RecipeItemInput
	ItemOutputs   []RecipeItemOutput
	FluidInputs   []RecipeFluidInput
	FluidOutputs  []RecipeFluidOutput
}

// BaseEUPerTick/MaxEUPerTick: EU throughput range
// MaxSlots: upgrade card slots available
// BaseSUMultiplier: nil if machine doesn't use SU energy
type MachineType struct {
	ModID            string
	MachineID        string
	Name             string
	BaseEUPerTick    int64
	MaxEUPerTick     int64
	MaxSlots         int16
	EnergyType       string
	BaseSUMultiplier *float64
}

// EUBonusPerSlot: how much EU capacity each card of this tier adds
// ItemModID/ItemID: the physical upgrade card item
type UpgradeTier struct {
	ID             string
	ModID          string
	Name           string
	EUBonusPerSlot int64
	ItemModID      string
	ItemID         string
}

// All recipes that output this item, highest priority first.
func (db *DB) GetRecipesForItem(ctx context.Context, itemModID, itemID string) ([]*RecipeWithIO, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT r.id, r.machine_mod_id, r.machine_id,
		                r.duration_ticks, r.total_eu, r.priority
		FROM recipes r
		JOIN recipe_item_outputs rio ON rio.recipe_id = r.id
		WHERE rio.item_mod_id = $1 AND rio.item_id = $2
		ORDER BY r.priority DESC
	`, itemModID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recipes []*RecipeWithIO
	for rows.Next() {
		r := &RecipeWithIO{}
		if err := rows.Scan(&r.ID, &r.MachineMod, &r.MachineID,
			&r.DurationTicks, &r.TotalEU, &r.Priority); err != nil {
			return nil, err
		}
		recipes = append(recipes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// hydrate inputs/outputs for each recipe header
	for _, r := range recipes {
		if err := db.loadRecipeIO(ctx, r); err != nil {
			return nil, err
		}
	}
	return recipes, nil
}

// Recipe by UUID
func (db *DB) GetRecipe(ctx context.Context, recipeID string) (*RecipeWithIO, error) {
	r := &RecipeWithIO{}
	err := db.Pool.QueryRow(ctx, `
		SELECT id, machine_mod_id, machine_id, duration_ticks, total_eu, priority
		FROM recipes WHERE id = $1
	`, recipeID).Scan(&r.ID, &r.MachineMod, &r.MachineID,
		&r.DurationTicks, &r.TotalEU, &r.Priority)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r, db.loadRecipeIO(ctx, r)
}

func (db *DB) GetMachineType(ctx context.Context, modID, machineID string) (*MachineType, error) {
	m := &MachineType{}
	err := db.Pool.QueryRow(ctx, `
		SELECT mod_id, machine_id, name,
		       COALESCE(base_eu_per_tick, 0),
		       COALESCE(max_eu_per_tick, 0),
		       COALESCE(max_slots, 0),
		       energy_type,
		       base_su_multiplier
		FROM machine_types
		WHERE mod_id = $1 AND machine_id = $2
	`, modID, machineID).Scan(
		&m.ModID, &m.MachineID, &m.Name,
		&m.BaseEUPerTick, &m.MaxEUPerTick, &m.MaxSlots,
		&m.EnergyType, &m.BaseSUMultiplier,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// All upgrade tiers for a mod, cheapest (lowest EU bonus) first.
func (db *DB) GetUpgradeTiers(ctx context.Context, modID string) ([]*UpgradeTier, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, mod_id, name, eu_bonus_per_slot, item_ref
		FROM upgrade_tiers
		WHERE mod_id = $1
		ORDER BY eu_bonus_per_slot ASC
	`, modID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tiers []*UpgradeTier
	for rows.Next() {
		t := &UpgradeTier{}
		var itemRef string // raw "mod_id:item_id" string from DB
		if err := rows.Scan(&t.ID, &t.ModID, &t.Name, &t.EUBonusPerSlot, &itemRef); err != nil {
			return nil, err
		}
		t.ItemModID, t.ItemID = splitItemRef(itemRef)
		tiers = append(tiers, t)
	}
	return tiers, rows.Err()
}

// Loads all item and fluid inputs/outputs for a recipe and fills the corresponding slices in RecipeWithIO.
func (db *DB) loadRecipeIO(ctx context.Context, r *RecipeWithIO) error {
	// Item Inputs
	rows, err := db.Pool.Query(ctx, `
		SELECT item_mod_id, item_id, tag_id,
		       amount_num, amount_den, probability_num, probability_den
		FROM recipe_item_inputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var in RecipeItemInput
		if err := rows.Scan(&in.ItemModID, &in.ItemID, &in.TagID,
			&in.AmountNum, &in.AmountDen, &in.ProbabilityNum, &in.ProbabilityDen); err != nil {
			return err
		}
		r.ItemInputs = append(r.ItemInputs, in)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Item Outputs
	rows2, err := db.Pool.Query(ctx, `
		SELECT item_mod_id, item_id, amount_num, amount_den, probability_num, probability_den
		FROM recipe_item_outputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return err
	}
	defer rows2.Close()
	for rows2.Next() {
		var out RecipeItemOutput
		if err := rows2.Scan(&out.ItemModID, &out.ItemID,
			&out.AmountNum, &out.AmountDen, &out.ProbabilityNum, &out.ProbabilityDen); err != nil {
			return err
		}
		r.ItemOutputs = append(r.ItemOutputs, out)
	}
	if err := rows2.Err(); err != nil {
		return err
	}

	// Fluid Inputs
	rows3, err := db.Pool.Query(ctx, `
		SELECT fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den
		FROM recipe_fluid_inputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return err
	}
	defer rows3.Close()
	for rows3.Next() {
		var fi RecipeFluidInput
		if err := rows3.Scan(&fi.FluidModID, &fi.FluidID,
			&fi.AmountMB, &fi.ProbabilityNum, &fi.ProbabilityDen); err != nil {
			return err
		}
		r.FluidInputs = append(r.FluidInputs, fi)
	}
	if err := rows3.Err(); err != nil {
		return err
	}

	// Fluid Outputs
	rows4, err := db.Pool.Query(ctx, `
		SELECT fluid_mod_id, fluid_id, amount_mb, probability_num, probability_den
		FROM recipe_fluid_outputs WHERE recipe_id = $1
	`, r.ID)
	if err != nil {
		return err
	}
	defer rows4.Close()
	for rows4.Next() {
		var fo RecipeFluidOutput
		if err := rows4.Scan(&fo.FluidModID, &fo.FluidID,
			&fo.AmountMB, &fo.ProbabilityNum, &fo.ProbabilityDen); err != nil {
			return err
		}
		r.FluidOutputs = append(r.FluidOutputs, fo)
	}
	return rows4.Err()
}

// splits "mod_id:item_id" → two strings; returns (ref, "") if no colon found
func splitItemRef(ref string) (modID, itemID string) {
	for i, c := range ref {
		if c == ':' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, ""
}
