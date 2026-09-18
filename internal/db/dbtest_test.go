package db

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
)

// testDB opens a pool against DATABASE_URL and skips the test if that variable is unset.
func testDB(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	d, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// seedMod inserts a minimal row into mods and removes it (and anything that cascades from
// it) via t.Cleanup.
func seedMod(t *testing.T, d *DB, modID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.Pool.Exec(ctx, `INSERT INTO mods (mod_id, name) VALUES ($1, $1)`, modID); err != nil {
		t.Fatalf("seed mod %q: %v", modID, err)
	}
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(), `DELETE FROM mods WHERE mod_id = $1`, modID); err != nil {
			t.Errorf("cleanup mod %q: %v", modID, err)
		}
	})
}

// seedMachineType inserts a minimal row into machine_types (mod must already exist) and
// removes it via t.Cleanup.
func seedMachineType(t *testing.T, d *DB, modID, machineID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.Pool.Exec(ctx,
		`INSERT INTO machine_types (mod_id, machine_id, name) VALUES ($1, $2, $2)`,
		modID, machineID); err != nil {
		t.Fatalf("seed machine type %s/%s: %v", modID, machineID, err)
	}
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(),
			`DELETE FROM machine_types WHERE mod_id = $1 AND machine_id = $2`,
			modID, machineID); err != nil {
			t.Errorf("cleanup machine type %s/%s: %v", modID, machineID, err)
		}
	})
}

// seedRecipe inserts a minimal row into recipes for an existing machine type and removes it
// via t.Cleanup. Returns the generated recipe ID.
func seedRecipe(t *testing.T, d *DB, machineModID, machineID, sourceModID string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var recipeID uuid.UUID
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO recipes (machine_mod_id, machine_id, source_mod_id, duration_ticks)
		VALUES ($1, $2, $3, 1)
		RETURNING id
	`, machineModID, machineID, sourceModID).Scan(&recipeID); err != nil {
		t.Fatalf("seed recipe: %v", err)
	}
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(), `DELETE FROM recipes WHERE id = $1`, recipeID); err != nil {
			t.Errorf("cleanup recipe %s: %v", recipeID, err)
		}
	})
	return recipeID
}

// seedSave inserts a save owned by an existing user and removes it via t.Cleanup.
func seedSave(t *testing.T, d *DB) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var userID uuid.UUID
	if err := d.Pool.QueryRow(ctx, `SELECT id FROM users LIMIT 1`).Scan(&userID); err != nil {
		t.Skip("no user in the dev DB to attach a test save to")
	}
	var saveID uuid.UUID
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO saves (user_id, name) VALUES ($1, $2) RETURNING id`,
		userID, "db-test-save").Scan(&saveID); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(), `DELETE FROM saves WHERE id = $1`, saveID); err != nil {
			t.Errorf("cleanup save %s: %v", saveID, err)
		}
	})
	return saveID
}

// seedFactory inserts a factory into an existing save.
func seedFactory(t *testing.T, d *DB, saveID uuid.UUID) uuid.UUID {
	t.Helper()
	var factoryID uuid.UUID
	if err := d.Pool.QueryRow(context.Background(),
		`INSERT INTO factories (save_id, name) VALUES ($1, 'db-test-factory') RETURNING id`,
		saveID).Scan(&factoryID); err != nil {
		t.Fatalf("seed factory: %v", err)
	}
	return factoryID
}

// seedProductionLine inserts a minimal production line into an existing factory.
func seedProductionLine(t *testing.T, d *DB, factoryID uuid.UUID) uuid.UUID {
	t.Helper()
	var plID uuid.UUID
	if err := d.Pool.QueryRow(context.Background(), `
		INSERT INTO production_lines
			(factory_id, target_mod_id, target_item_id, rate_num, rate_den, time_unit, optimize_mode)
		VALUES ($1, 'testmod', 'widget', 1, 1, 't', 'TARGET')
		RETURNING id
	`, factoryID).Scan(&plID); err != nil {
		t.Fatalf("seed production line: %v", err)
	}
	return plID
}
