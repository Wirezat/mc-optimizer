package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
)

// groupFixture seeds a mod, machine type, recipe, save and factory, and returns the factory
// plus the recipe the machine groups will reference.
func groupFixture(t *testing.T, d *DB) (factoryID uuid.UUID, recipeID uuid.UUID) {
	t.Helper()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeID = seedRecipe(t, d, "testmod", "test_machine", "testmod")
	return seedFactory(t, d, seedSave(t, d)), recipeID
}

func variantGroup(recipeID uuid.UUID) *model.MachineGroup {
	return &model.MachineGroup{
		MachineModID:  "testmod",
		MachineID:     "test_machine",
		RecipeID:      recipeID,
		Count:         4,
		Status:        "planned",
		ModConfig:     json.RawMessage(`{"tier":"steel"}`),
		VariantID:     "adv-x3",
		ExactCountNum: 7,
		ExactCountDen: 2,
	}
}

// assertVariantColumns checks a stored group against variantGroup's values.
func assertVariantColumns(t *testing.T, mg *model.MachineGroup) {
	t.Helper()
	if mg.VariantID != "adv-x3" {
		t.Errorf("variant_id = %q, want %q", mg.VariantID, "adv-x3")
	}
	if mg.CurrentVariantID != "default" {
		t.Errorf("current_variant_id = %q, want %q", mg.CurrentVariantID, "default")
	}
	var cfg map[string]any
	if err := json.Unmarshal(mg.ModConfig, &cfg); err != nil {
		t.Fatalf("unmarshal mod_config %q: %v", mg.ModConfig, err)
	}
	if cfg["tier"] != "steel" {
		t.Errorf("mod_config = %v, want tier steel", cfg)
	}
}

// Both insert paths must carry the plugin columns; a column dropped from one of them
// silently reverts the group to the host default.
func TestMachineGroupVariantColumnsRoundTrip(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	t.Run("confirm", func(t *testing.T) {
		factoryID, recipeID := groupFixture(t, d)
		detail, err := d.ConfirmSolverDraft(ctx, &model.ProductionLine{
			FactoryID: &factoryID, TargetModID: "testmod", TargetItemID: "widget",
			RateNum: 1, RateDen: 1, TimeUnit: "t", OptimizeMode: "TARGET",
			Status: "active", Position: "V",
		}, nil, []*model.MachineGroup{variantGroup(recipeID)}, uuid.New())
		if err != nil {
			t.Fatalf("confirm solver draft: %v", err)
		}
		mg, err := d.GetMachineGroup(ctx, detail.MachineGroups[0].ID)
		if err != nil {
			t.Fatalf("get machine group: %v", err)
		}
		assertVariantColumns(t, mg)
	})

	t.Run("replace", func(t *testing.T) {
		factoryID, recipeID := groupFixture(t, d)
		plID := seedProductionLine(t, d, factoryID)
		if _, err := d.ReplaceProductionLineContents(ctx, plID, 1, 1, "t", "TARGET",
			nil, []*model.MachineGroup{variantGroup(recipeID)}); err != nil {
			t.Fatalf("replace pl contents: %v", err)
		}
		groups, err := d.ListMachineGroupsByPL(ctx, plID)
		if err != nil {
			t.Fatalf("list machine groups: %v", err)
		}
		if len(groups) != 1 {
			t.Fatalf("got %d groups, want 1", len(groups))
		}
		assertVariantColumns(t, groups[0])
	})
}

// A group with no config override stores an empty object, which means "the save-wide config
// applies" — not SQL NULL, which the column forbids.
func TestMachineGroupWithoutConfigStoresEmptyObject(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	factoryID, recipeID := groupFixture(t, d)
	plID := seedProductionLine(t, d, factoryID)

	group := variantGroup(recipeID)
	group.ModConfig = nil
	if _, err := d.ReplaceProductionLineContents(ctx, plID, 1, 1, "t", "TARGET",
		nil, []*model.MachineGroup{group}); err != nil {
		t.Fatalf("replace pl contents: %v", err)
	}
	groups, err := d.ListMachineGroupsByPL(ctx, plID)
	if err != nil {
		t.Fatalf("list machine groups: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(groups[0].ModConfig, &cfg); err != nil {
		t.Fatalf("unmarshal mod_config %q: %v", groups[0].ModConfig, err)
	}
	if len(cfg) != 0 {
		t.Errorf("mod_config = %v, want an empty object", cfg)
	}
}

// Switching to a faster variant shrinks the group below its built count.
func TestUpdateMachineGroupVariantClampsBuiltCount(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	factoryID, recipeID := groupFixture(t, d)
	plID := seedProductionLine(t, d, factoryID)

	if _, err := d.ReplaceProductionLineContents(ctx, plID, 1, 1, "t", "TARGET",
		nil, []*model.MachineGroup{variantGroup(recipeID)}); err != nil {
		t.Fatalf("replace pl contents: %v", err)
	}
	groups, err := d.ListMachineGroupsByPL(ctx, plID)
	if err != nil {
		t.Fatalf("list machine groups: %v", err)
	}
	groupID := groups[0].ID
	if err := d.UpdateMachineGroupBuildState(ctx, groupID, 4); err != nil {
		t.Fatalf("update build state: %v", err)
	}

	if err := d.UpdateMachineGroupVariant(ctx, groupID, "eco", 1, 3, 4); err != nil {
		t.Fatalf("update variant: %v", err)
	}
	mg, err := d.GetMachineGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("get machine group: %v", err)
	}
	if mg.VariantID != "eco" || mg.Count != 1 {
		t.Errorf("variant_id/count = %q/%d, want %q/1", mg.VariantID, mg.Count, "eco")
	}
	if mg.ExactCountNum != 3 || mg.ExactCountDen != 4 {
		t.Errorf("exact count = %d/%d, want 3/4", mg.ExactCountNum, mg.ExactCountDen)
	}
	if mg.BuiltCount != 1 {
		t.Errorf("built_count = %d, want 1 (clamped to the new count)", mg.BuiltCount)
	}
}

func TestUpdateMachineGroupCurrentVariantIsIndependent(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	factoryID, recipeID := groupFixture(t, d)
	plID := seedProductionLine(t, d, factoryID)

	if _, err := d.ReplaceProductionLineContents(ctx, plID, 1, 1, "t", "TARGET",
		nil, []*model.MachineGroup{variantGroup(recipeID)}); err != nil {
		t.Fatalf("replace pl contents: %v", err)
	}
	groups, err := d.ListMachineGroupsByPL(ctx, plID)
	if err != nil {
		t.Fatalf("list machine groups: %v", err)
	}
	groupID := groups[0].ID

	if err := d.UpdateMachineGroupBuildState(ctx, groupID, 4); err != nil {
		t.Fatalf("update build state: %v", err)
	}
	mg, err := d.GetMachineGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("get machine group: %v", err)
	}
	if mg.CurrentVariantID != "default" {
		t.Errorf("current_variant_id = %q, want the untouched default", mg.CurrentVariantID)
	}
	if err := d.UpdateMachineGroupCurrentVariant(ctx, groupID, "adv-x3"); err != nil {
		t.Fatalf("update current variant: %v", err)
	}
	mg, err = d.GetMachineGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("get machine group: %v", err)
	}
	if mg.CurrentVariantID != "adv-x3" {
		t.Errorf("current_variant_id = %q, want %q", mg.CurrentVariantID, "adv-x3")
	}

	if err := d.UpdateMachineGroupVariant(ctx, groupID, "eco", 1, 3, 4); err != nil {
		t.Fatalf("update variant: %v", err)
	}
	mg, err = d.GetMachineGroup(ctx, groupID)
	if err != nil {
		t.Fatalf("get machine group: %v", err)
	}
	if mg.VariantID != "eco" {
		t.Errorf("variant_id = %q, want %q", mg.VariantID, "eco")
	}
	if mg.CurrentVariantID != "adv-x3" {
		t.Errorf("current_variant_id = %q, want %q (unchanged: nothing new was built yet)", mg.CurrentVariantID, "adv-x3")
	}
}

func TestUpdateMachineGroupVariantMissingGroup(t *testing.T) {
	d := testDB(t)
	if err := d.UpdateMachineGroupVariant(context.Background(), uuid.New(), "eco", 1, 1, 1); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The variant endpoint needs the save a group belongs to, to read that save's mod config,
// alongside the owner it authorises against.
func TestMachineGroupScopeReturnsOwnerAndSave(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")
	seedMachineType(t, d, "testmod", "test_machine")
	recipeID := seedRecipe(t, d, "testmod", "test_machine", "testmod")
	saveID := seedSave(t, d)
	plID := seedProductionLine(t, d, seedFactory(t, d, saveID))

	if _, err := d.ReplaceProductionLineContents(ctx, plID, 1, 1, "t", "TARGET",
		nil, []*model.MachineGroup{variantGroup(recipeID)}); err != nil {
		t.Fatalf("replace pl contents: %v", err)
	}
	groups, err := d.ListMachineGroupsByPL(ctx, plID)
	if err != nil {
		t.Fatalf("list machine groups: %v", err)
	}

	var wantUser uuid.UUID
	if err := d.Pool.QueryRow(ctx, `SELECT user_id FROM saves WHERE id = $1`, saveID).Scan(&wantUser); err != nil {
		t.Fatalf("read save owner: %v", err)
	}
	gotUser, gotSave, err := d.MachineGroupScope(ctx, groups[0].ID)
	if err != nil {
		t.Fatalf("machine group scope: %v", err)
	}
	if gotUser != wantUser {
		t.Errorf("user = %s, want %s", gotUser, wantUser)
	}
	if gotSave != saveID {
		t.Errorf("save = %s, want %s", gotSave, saveID)
	}
}

func TestMachineGroupScopeMissingGroup(t *testing.T) {
	d := testDB(t)
	if _, _, err := d.MachineGroupScope(context.Background(), uuid.New()); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
