package db

import (
	"context"
	"sort"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
	"github.com/Wirezat/production-optimizer/internal/solver"
	"github.com/google/uuid"
)

func cleanupTestTags(t *testing.T, d *DB) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := d.Pool.Exec(context.Background(), `DELETE FROM tags WHERE name LIKE 'r1test:%'`); err != nil {
			t.Errorf("cleanup tags: %v", err)
		}
	})
}

func tagMemberIDs(t *testing.T, d *DB, kind, name string) []string {
	t.Helper()
	table, col := "tag_members", "item_mod_id || ':' || item_id"
	if kind == model.TagKindFluid {
		table, col = "tag_fluid_members", "fluid_mod_id || ':' || fluid_id"
	}
	rows, err := d.Pool.Query(context.Background(), `
		SELECT `+col+` FROM `+table+` m JOIN tags t ON t.id = m.tag_id
		WHERE t.kind = $1 AND t.name = $2`, kind, name)
	if err != nil {
		t.Fatalf("select members: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func TestUpsertDirectTagMembers_SameNameForItemAndFluid(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	cleanupTestTags(t, d)
	seedMod(t, d, "r1tagmod")
	if err := d.UpsertFluids(ctx, "r1tagmod", []string{"honey"}); err != nil {
		t.Fatalf("upsert fluid: %v", err)
	}
	if _, err := d.Pool.Exec(ctx, `INSERT INTO items (mod_id, item_id) VALUES ('r1tagmod', 'honey_bottle')`); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1tagmod", model.TagKindFluid, "r1test:honey", []string{"r1tagmod:honey"}); err != nil {
		t.Fatalf("fluid tag: %v", err)
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1tagmod", model.TagKindItem, "r1test:honey", []string{"r1tagmod:honey_bottle"}); err != nil {
		t.Fatalf("item tag: %v", err)
	}
	if got := tagMemberIDs(t, d, model.TagKindFluid, "r1test:honey"); len(got) != 1 || got[0] != "r1tagmod:honey" {
		t.Errorf("fluid members = %v, want [r1tagmod:honey]", got)
	}
	if got := tagMemberIDs(t, d, model.TagKindItem, "r1test:honey"); len(got) != 1 || got[0] != "r1tagmod:honey_bottle" {
		t.Errorf("item members = %v, want [r1tagmod:honey_bottle]", got)
	}
}

func TestUpsertDirectTagMembers_ReimportKeepsOtherModsMembers(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	cleanupTestTags(t, d)
	seedMod(t, d, "r1moda")
	seedMod(t, d, "r1modb")
	for _, m := range []string{"r1moda", "r1modb"} {
		if err := d.UpsertFluids(ctx, m, []string{"honey", "nectar"}); err != nil {
			t.Fatalf("upsert fluids %s: %v", m, err)
		}
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1moda", model.TagKindFluid, "r1test:honey", []string{"r1moda:honey"}); err != nil {
		t.Fatalf("mod a: %v", err)
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1modb", model.TagKindFluid, "r1test:honey", []string{"r1modb:honey"}); err != nil {
		t.Fatalf("mod b: %v", err)
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1moda", model.TagKindFluid, "r1test:honey", []string{"r1moda:nectar"}); err != nil {
		t.Fatalf("mod a again: %v", err)
	}
	want := []string{"r1moda:nectar", "r1modb:honey"}
	if got := tagMemberIDs(t, d, model.TagKindFluid, "r1test:honey"); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("members after re-import of a = %v, want %v", got, want)
	}
	if _, err := d.Pool.Exec(ctx, `DELETE FROM mods WHERE mod_id = 'r1moda'`); err != nil {
		t.Fatalf("delete mod a: %v", err)
	}
	if got := tagMemberIDs(t, d, model.TagKindFluid, "r1test:honey"); len(got) != 1 || got[0] != "r1modb:honey" {
		t.Errorf("members after deleting a = %v, want [r1modb:honey]", got)
	}
}

func TestImportRecipe_FluidTagInputGetsAFluidTag(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	cleanupTestTags(t, d)
	seedMod(t, d, "r1importmod")
	seedMachineType(t, d, "r1importmod", "canner")
	if _, err := d.Pool.Exec(ctx, `INSERT INTO items (mod_id, item_id) VALUES ('r1importmod', 'honey_bottle')`); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	rec := model.NormalizedRecipe{
		ModID: "r1importmod", SourceModID: "r1importmod", MachineID: "canner", Duration: 100,
		ItemInputs: []model.NormalizedIO{
			{TagName: strp("r1test:honey"), AmountNum: 1, AmountDen: 1, ProbNum: 1, ProbDen: 1},
		},
		FluidInputs: []model.NormalizedIO{
			{TagName: strp("r1test:honey"), AmountMB: 250, ProbNum: 1, ProbDen: 1},
		},
		ItemOutputs: []model.NormalizedIO{
			{ModID: strp("r1importmod"), ID: strp("honey_bottle"), AmountNum: 1, AmountDen: 1, ProbNum: 1, ProbDen: 1},
		},
		ContentHash: uuid.NewString(),
	}
	if _, err := d.ImportRecipe(ctx, rec); err != nil {
		t.Fatalf("import recipe: %v", err)
	}
	var fluidKind, itemKind string
	if err := d.Pool.QueryRow(ctx, `
		SELECT t.kind FROM recipe_fluid_inputs f JOIN tags t ON t.id = f.tag_id
		JOIN recipes r ON r.id = f.recipe_id WHERE r.content_hash = $1`, rec.ContentHash).Scan(&fluidKind); err != nil {
		t.Fatalf("select fluid tag kind: %v", err)
	}
	if err := d.Pool.QueryRow(ctx, `
		SELECT t.kind FROM recipe_item_inputs i JOIN tags t ON t.id = i.tag_id
		JOIN recipes r ON r.id = i.recipe_id WHERE r.content_hash = $1`, rec.ContentHash).Scan(&itemKind); err != nil {
		t.Fatalf("select item tag kind: %v", err)
	}
	if fluidKind != model.TagKindFluid || itemKind != model.TagKindItem {
		t.Errorf("kinds = fluid input %q, item input %q; want fluid, item", fluidKind, itemKind)
	}
}

func TestFluidTag_LoadsAndResolves(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	cleanupTestTags(t, d)
	seedMod(t, d, "r1loadmod")
	seedMachineType(t, d, "r1loadmod", "canner")
	if err := d.UpsertFluids(ctx, "r1loadmod", []string{"honey"}); err != nil {
		t.Fatalf("upsert fluid: %v", err)
	}
	if _, err := d.Pool.Exec(ctx, `INSERT INTO items (mod_id, item_id) VALUES ('r1loadmod', 'honey_bottle')`); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if err := d.UpsertDirectTagMembers(ctx, "r1loadmod", model.TagKindFluid, "r1test:honey", []string{"r1loadmod:honey"}); err != nil {
		t.Fatalf("fluid tag: %v", err)
	}
	rec := model.NormalizedRecipe{
		ModID: "r1loadmod", SourceModID: "r1loadmod", MachineID: "canner", Duration: 100,
		FluidInputs: []model.NormalizedIO{
			{TagName: strp("r1test:honey"), AmountMB: 250, ProbNum: 1, ProbDen: 1},
		},
		ItemOutputs: []model.NormalizedIO{
			{ModID: strp("r1loadmod"), ID: strp("honey_bottle"), AmountNum: 1, AmountDen: 1, ProbNum: 1, ProbDen: 1},
		},
		ContentHash: uuid.NewString(),
	}
	if _, err := d.ImportRecipe(ctx, rec); err != nil {
		t.Fatalf("import recipe: %v", err)
	}
	var recipeID string
	if err := d.Pool.QueryRow(ctx, `SELECT id::text FROM recipes WHERE content_hash = $1`, rec.ContentHash).Scan(&recipeID); err != nil {
		t.Fatalf("select recipe: %v", err)
	}
	r, err := d.GetRecipe(ctx, recipeID)
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	if len(r.FluidInputs) != 1 || r.FluidInputs[0].TagName == nil || *r.FluidInputs[0].TagName != "r1test:honey" {
		t.Fatalf("fluid inputs = %+v, want the tag r1test:honey", r.FluidInputs)
	}
	members, err := d.GetTagMembers(ctx, solver.ResourceRef{TagRef: "r1test:honey", Kind: resource.KindFluid})
	if err != nil {
		t.Fatalf("GetTagMembers: %v", err)
	}
	if len(members) != 1 || members[0].Key() != "fluid:r1loadmod:honey" {
		t.Errorf("members = %+v, want fluid r1loadmod:honey", members)
	}
	itemMembers, err := d.GetTagMembers(ctx, solver.ResourceRef{TagRef: "r1test:honey"})
	if err != nil {
		t.Fatalf("GetTagMembers item: %v", err)
	}
	if len(itemMembers) != 0 {
		t.Errorf("item tag r1test:honey members = %+v, want none", itemMembers)
	}
}
