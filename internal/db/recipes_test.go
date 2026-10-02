package db

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// A machine that implements another via machine_interfaces can run that machine's recipes,
// and both must be offered as separate candidates.
func TestGetRecipesForOffersInterfaceMachines(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	seedMod(t, d, "grfi_mod")
	seedMachineType(t, d, "grfi_mod", "base_machine")
	seedMachineType(t, d, "grfi_mod", "tier_machine")
	recipeID := seedRecipe(t, d, "grfi_mod", "base_machine", "grfi_mod")

	if _, err := d.Pool.Exec(ctx,
		`INSERT INTO items (mod_id, item_id) VALUES ($1, $2)`,
		"grfi_mod", "widget"); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO recipe_item_outputs (recipe_id, item_mod_id, item_id, amount_num, amount_den, probability_num, probability_den)
		VALUES ($1, $2, $3, 1, 1, 1, 1)
	`, recipeID, "grfi_mod", "widget"); err != nil {
		t.Fatalf("seed recipe output: %v", err)
	}
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO machine_interfaces (base_mod_id, base_machine_id, machine_mod_id, machine_id)
		VALUES ($1, $2, $3, $4)
	`, "grfi_mod", "base_machine", "grfi_mod", "tier_machine"); err != nil {
		t.Fatalf("seed machine interface: %v", err)
	}
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(),
			`DELETE FROM machine_interfaces WHERE base_mod_id = $1`, "grfi_mod"); err != nil {
			t.Errorf("cleanup machine_interfaces: %v", err)
		}
	})

	got, err := d.GetRecipesFor(ctx, resource.Ref{ModID: "grfi_mod", ID: "widget"})
	if err != nil {
		t.Fatalf("GetRecipesFor: %v", err)
	}
	if len(got) != 2 {
		var seen []string
		for _, r := range got {
			seen = append(seen, r.MachineMod+":"+r.MachineID)
		}
		t.Fatalf("got %d candidates %v, want 2 (base and its interface implementer)", len(got), seen)
	}
	if got[0].MachineID != "base_machine" {
		t.Errorf("candidates[0] = %q, want \"base_machine\" first", got[0].MachineID)
	}
	if got[1].MachineID != "tier_machine" {
		t.Errorf("candidates[1] = %q, want \"tier_machine\"", got[1].MachineID)
	}
	if got[0].ID != got[1].ID {
		t.Errorf("candidates carry different recipe ids (%s, %s); both must be the same recipe", got[0].ID, got[1].ID)
	}
}
