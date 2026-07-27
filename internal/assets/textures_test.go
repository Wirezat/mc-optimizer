package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, root, rel string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestResolveItemTexture_ItemPresent(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/textures/item/motor.png")

	url, ok := ResolveItemTexture(dir, "modern_industrialization", "motor")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "/assets/modern_industrialization/textures/item/motor.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveItemTexture_BlockItemFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/textures/block/bronze_block.png")

	url, ok := ResolveItemTexture(dir, "modern_industrialization", "bronze_block")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "/assets/modern_industrialization/textures/block/bronze_block.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveItemTexture_ItemWinsOverBlock(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/textures/item/dual.png")
	writeFixture(t, dir, "modern_industrialization/textures/block/dual.png")

	url, ok := ResolveItemTexture(dir, "modern_industrialization", "dual")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "/assets/modern_industrialization/textures/item/dual.png"
	if url != want {
		t.Errorf("url = %q, want %q (item must win over block)", url, want)
	}
}

func TestResolveItemTexture_Missing(t *testing.T) {
	dir := t.TempDir()

	_, ok := ResolveItemTexture(dir, "modern_industrialization", "nonexistent")
	if ok {
		t.Fatal("expected ok=false for missing texture")
	}
}

// modID/itemID come from the DB, not straight off a request, but a resolver
// that turns them into a path should still refuse to walk out of assetsDir.
func TestResolveItemTexture_RejectsEscapingSegments(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "secret.png")

	for _, tc := range []struct{ modID, itemID string }{
		{"..", "secret"},
		{"modern_industrialization", ".."},
		{"../modern_industrialization", "motor"},
		{"modern_industrialization", "../secret"},
		{"", "motor"},
		{"modern_industrialization", ""},
	} {
		if _, ok := ResolveItemTexture(dir, tc.modID, tc.itemID); ok {
			t.Errorf("ResolveItemTexture(%q, %q) = ok, want rejected", tc.modID, tc.itemID)
		}
	}
}

func TestResolveFluidTexture_Present(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/textures/fluid/steam_still.png")

	url, ok := ResolveFluidTexture(dir, "modern_industrialization", "steam")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "/assets/modern_industrialization/textures/fluid/steam_still.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveFluidTexture_RejectsEscapingSegments(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "secret.png")

	if _, ok := ResolveFluidTexture(dir, "..", "steam"); ok {
		t.Error(`ResolveFluidTexture("..", "steam") = ok, want rejected`)
	}
	if _, ok := ResolveFluidTexture(dir, "modern_industrialization", "../secret"); ok {
		t.Error(`ResolveFluidTexture(mod, "../secret") = ok, want rejected`)
	}
}

func TestResolveFluidTexture_Missing(t *testing.T) {
	dir := t.TempDir()

	_, ok := ResolveFluidTexture(dir, "modern_industrialization", "nonexistent")
	if ok {
		t.Fatal("expected ok=false for missing texture")
	}
}

// Some items have no sprite at all because the game draws a 3D model for them —
// MI's pipes and cables. The resolver points those at the render endpoint, so a
// caller can put the URL in an <img> without knowing which kind it got.
func TestResolveItemTexture_ModelFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/models/item/copper_cable.json")

	url, ok := ResolveItemTexture(dir, "modern_industrialization", "copper_cable")
	if !ok {
		t.Fatal("expected ok=true for an item that only has a model")
	}
	want := "/assets/render/modern_industrialization/item/copper_cable.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

// A mod can omit a block-item's own item model and let it fall back to the
// block's blockstate; the resolver only needs the file to exist, the render
// endpoint does the actual blockstate lookup.
func TestResolveItemTexture_BlockStateFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/blockstates/plain_block.json")

	url, ok := ResolveItemTexture(dir, "mod", "plain_block")
	if !ok {
		t.Fatal("expected ok=true for an item backed only by a blockstate")
	}
	want := "/assets/render/mod/item/plain_block.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

// A real sprite always beats a model: rendering one is work, and a mod that
// ships both means the sprite is the intended icon.
func TestResolveItemTexture_TextureWinsOverModel(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/textures/item/thing.png")
	writeFixture(t, dir, "mod/models/item/thing.json")

	url, _ := ResolveItemTexture(dir, "mod", "thing")
	if want := "/assets/mod/textures/item/thing.png"; url != want {
		t.Errorf("url = %q, want the flat texture %q", url, want)
	}
}

// Mods keep fluid art under textures/fluid/, but vanilla's own water and lava
// live in textures/block/. Missing this leaves minecraft:water with no icon.
func TestResolveFluidTexture_VanillaBlockFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "minecraft/textures/block/water_still.png")

	url, ok := ResolveFluidTexture(dir, "minecraft", "water")
	if !ok {
		t.Fatal("expected ok=true for vanilla's block-layout fluid texture")
	}
	want := "/assets/minecraft/textures/block/water_still.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

// The mod layout wins where both exist, so a mod cannot be shadowed by an
// unrelated block texture that happens to share the name.
func TestResolveFluidTexture_FluidDirWinsOverBlock(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/textures/fluid/steam_still.png")
	writeFixture(t, dir, "mod/textures/block/steam_still.png")

	url, _ := ResolveFluidTexture(dir, "mod", "steam")
	if want := "/assets/mod/textures/fluid/steam_still.png"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}
