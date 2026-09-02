package db

import (
	"context"
	"testing"
)

func TestUpsertAndGetModPlugin(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")

	in := ModPlugin{
		ModID: "testmod", DisplayName: "Test Mod", Version: "1.0.0",
		APIVersion: 1, Source: "var plugin = {}", HasWizard: true, UploadedBy: "tester",
	}
	if err := d.UpsertModPlugin(ctx, in); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := d.GetModPlugin(ctx, "testmod")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != "1.0.0" || got.APIVersion != 1 || !got.HasWizard {
		t.Errorf("got %+v, want version 1.0.0 / api 1 / wizard true", got)
	}
	if got.Source != in.Source {
		t.Errorf("Source = %q, want %q", got.Source, in.Source)
	}
	if got.UploadedBy != "tester" {
		t.Errorf("UploadedBy = %q, want %q", got.UploadedBy, "tester")
	}
}

func TestUpsertModPluginReplaces(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")

	base := ModPlugin{ModID: "testmod", DisplayName: "Test Mod", Version: "1.0.0", APIVersion: 1, Source: "a"}
	if err := d.UpsertModPlugin(ctx, base); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	base.Version, base.Source = "2.0.0", "b"
	if err := d.UpsertModPlugin(ctx, base); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err := d.GetModPlugin(ctx, "testmod")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != "2.0.0" || got.Source != "b" {
		t.Errorf("got %s/%s, want 2.0.0/b", got.Version, got.Source)
	}
}

func TestUpsertModPluginPreservesUploadedBy(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod")

	// Insert with UploadedBy set.
	first := ModPlugin{
		ModID: "testmod", DisplayName: "Test Mod", Version: "1.0.0",
		APIVersion: 1, Source: "a", UploadedBy: "tester",
	}
	if err := d.UpsertModPlugin(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Upsert again with empty UploadedBy; should preserve the original.
	second := ModPlugin{
		ModID: "testmod", DisplayName: "Test Mod", Version: "2.0.0",
		APIVersion: 1, Source: "b", UploadedBy: "",
	}
	if err := d.UpsertModPlugin(ctx, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := d.GetModPlugin(ctx, "testmod")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UploadedBy != "tester" {
		t.Errorf("UploadedBy = %q, want %q after re-upsert without UploadedBy", got.UploadedBy, "tester")
	}
}

func TestGetModPluginNotFound(t *testing.T) {
	d := testDB(t)
	if _, err := d.GetModPlugin(context.Background(), "absent"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListModPlugins(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedMod(t, d, "testmod_a")
	seedMod(t, d, "testmod_b")
	seedMod(t, d, "testmod_c")

	// Insertion order is deliberately not sorted and not reverse-sorted
	// relative to mod_id, so ordering by insertion time (ascending or
	// descending) cannot coincidentally reproduce mod_id order.
	for _, id := range []string{"testmod_b", "testmod_a", "testmod_c"} {
		if err := d.UpsertModPlugin(ctx, ModPlugin{ModID: id, DisplayName: id, Version: "1", APIVersion: 1, Source: id}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	all, err := d.ListModPlugins(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got []string
	for _, p := range all {
		if p.ModID == "testmod_a" || p.ModID == "testmod_b" || p.ModID == "testmod_c" {
			got = append(got, p.ModID)
		}
	}
	want := []string{"testmod_a", "testmod_b", "testmod_c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v (mod_id ascending order)", got, want)
			break
		}
	}
}
