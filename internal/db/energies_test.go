package db

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
)

func energyMap(t *testing.T, d *DB, modID string) map[string]*model.Energy {
	t.Helper()
	all, err := d.ListAllEnergies(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListAllEnergies: %v", err)
	}
	out := map[string]*model.Energy{}
	for _, e := range all {
		if e.ModID == modID {
			out[e.EnergyID] = e
		}
	}
	return out
}

func TestReplaceEnergies_UpsertsAndDropsMissing(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "energy-test")

	first := []model.EnergyDef{
		{ModID: "energy-test", EnergyID: "eu", Symbol: "EU", FePerUnit: resource.NewRational(10, 1)},
		{ModID: "energy-test", EnergyID: "old", Symbol: "OLD", FePerUnit: resource.NewRational(1, 1)},
	}
	if err := d.ReplaceEnergies(ctx, "energy-test", first); err != nil {
		t.Fatalf("ReplaceEnergies: %v", err)
	}
	if got := energyMap(t, d, "energy-test"); len(got) != 2 {
		t.Fatalf("after first import = %v, want eu and old", got)
	}

	second := []model.EnergyDef{
		{ModID: "energy-test", EnergyID: "eu", Symbol: "EU", FePerUnit: resource.NewRational(1, 4)},
	}
	if err := d.ReplaceEnergies(ctx, "energy-test", second); err != nil {
		t.Fatalf("ReplaceEnergies: %v", err)
	}
	got := energyMap(t, d, "energy-test")
	if len(got) != 1 || got["eu"] == nil {
		t.Fatalf("after re-import = %v, want only eu", got)
	}
	if f := got["eu"].FePerUnit; f.Num != 1 || f.Den != 4 {
		t.Errorf("fe_per_unit = %d/%d, want 1/4", f.Num, f.Den)
	}
}

func TestReplaceEnergies_RejectsAFormOfAnotherMod(t *testing.T) {
	d := testDB(t)
	seedMod(t, d, "energy-test")
	err := d.ReplaceEnergies(context.Background(), "energy-test", []model.EnergyDef{
		{ModID: "energy-elsewhere", EnergyID: "eu", Symbol: "EU", FePerUnit: resource.NewRational(1, 1)},
	})
	if err == nil {
		t.Fatal("ReplaceEnergies succeeded, want an error")
	}
}

func TestListAllEnergies_NameFromTranslationsElseSymbol(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "energy-test")
	if err := d.UpsertTranslations(ctx, "en_us", map[string]string{"text.energy-test.named": "Named Energy"}); err != nil {
		t.Fatalf("UpsertTranslations: %v", err)
	}
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM translations WHERE lang_key = 'text.energy-test.named'`)
	})
	if err := d.ReplaceEnergies(ctx, "energy-test", []model.EnergyDef{
		{ModID: "energy-test", EnergyID: "named", Symbol: "NE", LangKey: "text.energy-test.named", FePerUnit: resource.NewRational(1, 1)},
		{ModID: "energy-test", EnergyID: "bare", Symbol: "BE", FePerUnit: resource.NewRational(1, 1)},
		{ModID: "energy-test", EnergyID: "dangling", Symbol: "DE", LangKey: "text.energy-test.missing", FePerUnit: resource.NewRational(1, 1)},
	}); err != nil {
		t.Fatalf("ReplaceEnergies: %v", err)
	}
	got := energyMap(t, d, "energy-test")
	for id, want := range map[string]string{"named": "Named Energy", "bare": "BE", "dangling": "DE"} {
		if got[id] == nil || got[id].Name != want {
			t.Errorf("%s name = %+v, want %q", id, got[id], want)
		}
	}
}

func TestListAllEnergies_FiltersByActiveModsOfASave(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "energy-active")
	seedMod(t, d, "energy-inactive")
	for _, mod := range []string{"energy-active", "energy-inactive"} {
		if err := d.ReplaceEnergies(ctx, mod, []model.EnergyDef{
			{ModID: mod, EnergyID: "eu", Symbol: "EU", FePerUnit: resource.NewRational(1, 1)},
		}); err != nil {
			t.Fatalf("ReplaceEnergies %s: %v", mod, err)
		}
	}
	saveID := seedSave(t, d)
	if _, err := d.Pool.Exec(ctx, `INSERT INTO save_active_mods (save_id, mod_id) VALUES ($1, 'energy-active')`, saveID); err != nil {
		t.Fatalf("activate mod: %v", err)
	}
	all, err := d.ListAllEnergies(ctx, &saveID)
	if err != nil {
		t.Fatalf("ListAllEnergies: %v", err)
	}
	mods := map[string]bool{}
	for _, e := range all {
		mods[e.ModID] = true
	}
	if !mods["energy-active"] || mods["energy-inactive"] {
		t.Errorf("mods = %v, want energy-active only", mods)
	}
}

func TestDeleteMod_CascadesToEnergies(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "energy-test")
	if err := d.ReplaceEnergies(ctx, "energy-test", []model.EnergyDef{
		{ModID: "energy-test", EnergyID: "eu", Symbol: "EU", FePerUnit: resource.NewRational(1, 1)},
	}); err != nil {
		t.Fatalf("ReplaceEnergies: %v", err)
	}
	if err := d.DeleteMod(ctx, "energy-test"); err != nil {
		t.Fatalf("DeleteMod: %v", err)
	}
	if got := energyMap(t, d, "energy-test"); len(got) != 0 {
		t.Errorf("energies after mod delete = %v, want none", got)
	}
}
