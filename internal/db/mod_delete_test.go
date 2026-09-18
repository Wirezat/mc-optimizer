package db

import (
	"context"
	"errors"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// kinds returns the blocker kinds of a ModUsage result, for comparing against what a test
// expects without depending on counts or samples.
func kinds(bs []model.ModBlocker) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Kind)
	}
	return out
}

func hasKind(bs []model.ModBlocker, kind string) bool {
	for _, b := range bs {
		if b.Kind == kind {
			return true
		}
	}
	return false
}

func TestModUsageUnusedModHasNoBlockers(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-free")

	got, err := d.ModUsage(context.Background(), "usage-free")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("blockers = %v, want none", kinds(got))
	}
}

func TestDeleteModRemovesAnUnusedMod(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-deletable")

	if err := d.DeleteMod(context.Background(), "usage-deletable"); err != nil {
		t.Fatalf("DeleteMod: %v", err)
	}
	var n int
	if err := d.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM mods WHERE mod_id = 'usage-deletable'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("mod still present after delete")
	}
}

func TestDeleteModMissingModIsNotFound(t *testing.T) {
	d := testDB(t)
	if err := d.DeleteMod(context.Background(), "usage-does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A machine group pins a machine and a recipe, so a production line built on this mod's
// machines blocks deletion.
func TestModUsageBlocksOnMachineGroup(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-mg")
	seedMachineType(t, d, "usage-mg", "press")
	recipeID := seedRecipe(t, d, "usage-mg", "press", "usage-mg")
	saveID := seedSave(t, d)
	factoryID := seedFactory(t, d, saveID)
	plID := seedProductionLine(t, d, factoryID)
	seedMachineGroup(t, d, plID, "usage-mg", "press", recipeID)

	got, err := d.ModUsage(context.Background(), "usage-mg")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if !hasKind(got, "production_line") {
		t.Fatalf("blockers = %v, want production_line", kinds(got))
	}
}

// A production line producing this mod's item blocks it even with no machine group, because
// production_lines.target_mod_id carries no foreign key.
func TestModUsageBlocksOnProductionLineTarget(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-target")
	saveID := seedSave(t, d)
	factoryID := seedFactory(t, d, saveID)
	plID := seedProductionLine(t, d, factoryID)
	if _, err := d.Pool.Exec(context.Background(),
		`UPDATE production_lines SET target_mod_id = 'usage-target' WHERE id = $1`, plID); err != nil {
		t.Fatalf("point production line at mod: %v", err)
	}

	got, err := d.ModUsage(context.Background(), "usage-target")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if !hasKind(got, "production_line") {
		t.Fatalf("blockers = %v, want production_line", kinds(got))
	}
}

// pl_io rows carry no foreign key either.
func TestModUsageBlocksOnProductionLineIO(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-plio")
	saveID := seedSave(t, d)
	factoryID := seedFactory(t, d, saveID)
	plID := seedProductionLine(t, d, factoryID)
	if _, err := d.Pool.Exec(context.Background(), `
		INSERT INTO pl_io (pl_id, direction, io_type, mod_id, item_fluid_id, rate_num, rate_den)
		VALUES ($1, 'input', 'item', 'usage-plio', 'widget', 1, 1)
	`, plID); err != nil {
		t.Fatalf("seed pl_io: %v", err)
	}

	got, err := d.ModUsage(context.Background(), "usage-plio")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if !hasKind(got, "production_line") {
		t.Fatalf("blockers = %v, want production_line", kinds(got))
	}
}

// Manual-mode rows carry no foreign key.
func TestModUsageBlocksOnFactorySourceIO(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-src")
	saveID := seedSave(t, d)
	factoryID := seedFactory(t, d, saveID)
	if _, err := d.Pool.Exec(context.Background(), `
		INSERT INTO factory_source_inputs (factory_id, mod_id, item_id, rate_num, rate_den, time_unit)
		VALUES ($1, 'usage-src', 'widget', 1, 1, 'min')
	`, factoryID); err != nil {
		t.Fatalf("seed factory source input: %v", err)
	}

	got, err := d.ModUsage(context.Background(), "usage-src")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if !hasKind(got, "factory_source") {
		t.Fatalf("blockers = %v, want factory_source", kinds(got))
	}
}

// save_active_mods cascades, so without this check the mod would vanish out of a save
// silently.
func TestModUsageBlocksOnActiveInASave(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-active")
	saveID := seedSave(t, d)
	if _, err := d.Pool.Exec(context.Background(),
		`INSERT INTO save_active_mods (save_id, mod_id) VALUES ($1, 'usage-active')`, saveID); err != nil {
		t.Fatalf("seed active mod: %v", err)
	}

	got, err := d.ModUsage(context.Background(), "usage-active")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if !hasKind(got, "save") {
		t.Fatalf("blockers = %v, want save", kinds(got))
	}
}

// A recipe of another mod running on this mod's machine would cascade away with it.
func TestModUsageBlocksOnAnotherModsRecipe(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-base")
	seedMod(t, d, "usage-dependent")
	seedMachineType(t, d, "usage-base", "oven")
	seedRecipe(t, d, "usage-base", "oven", "usage-dependent")

	got, err := d.ModUsage(context.Background(), "usage-base")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if !hasKind(got, "dependent_mod") {
		t.Fatalf("blockers = %v, want dependent_mod", kinds(got))
	}
}

// A machine of another mod implementing this mod's base machine would lose that declaration.
func TestModUsageBlocksOnAnotherModsMachineInterface(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-iface-base")
	seedMod(t, d, "usage-iface-dep")
	seedMachineType(t, d, "usage-iface-base", "furnace")
	seedMachineType(t, d, "usage-iface-dep", "steel_furnace")
	if _, err := d.Pool.Exec(context.Background(), `
		INSERT INTO machine_interfaces (machine_mod_id, machine_id, base_mod_id, base_machine_id)
		VALUES ('usage-iface-dep', 'steel_furnace', 'usage-iface-base', 'furnace')
	`); err != nil {
		t.Fatalf("seed machine interface: %v", err)
	}

	got, err := d.ModUsage(context.Background(), "usage-iface-base")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if !hasKind(got, "dependent_mod") {
		t.Fatalf("blockers = %v, want dependent_mod", kinds(got))
	}
}

// A mod's own recipes and machines are not a reason to keep it.
func TestModUsageIgnoresTheModsOwnCatalog(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-self")
	seedMachineType(t, d, "usage-self", "mill")
	seedRecipe(t, d, "usage-self", "mill", "usage-self")

	got, err := d.ModUsage(context.Background(), "usage-self")
	if err != nil {
		t.Fatalf("ModUsage: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("blockers = %v, want none for a mod's own catalog", kinds(got))
	}
}

// DeleteMod refuses a used mod and leaves it in place.
func TestDeleteModRefusesAUsedMod(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "usage-refuse-base")
	seedMod(t, d, "usage-refuse-dep")
	seedMachineType(t, d, "usage-refuse-base", "oven")
	seedRecipe(t, d, "usage-refuse-base", "oven", "usage-refuse-dep")

	err := d.DeleteMod(context.Background(), "usage-refuse-base")
	var inUse *ErrModInUse
	if !errors.As(err, &inUse) {
		t.Fatalf("err = %v, want *ErrModInUse", err)
	}
	if !hasKind(inUse.Blockers, "dependent_mod") {
		t.Fatalf("blockers = %v, want dependent_mod", kinds(inUse.Blockers))
	}
	var n int
	if err := d.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM mods WHERE mod_id = 'usage-refuse-base'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("mod row count = %d, want it left in place", n)
	}
}
