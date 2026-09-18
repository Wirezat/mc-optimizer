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

func TestResolveItemTexture_ModelWinsOverTexture(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/textures/item/thing.png")
	writeFixture(t, dir, "mod/models/item/thing.json")

	url, _ := ResolveItemTexture(dir, "mod", "thing")
	if want := "/assets/render/mod/item/thing.png"; url != want {
		t.Errorf("url = %q, want the model render %q", url, want)
	}
}

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

func TestResolveFluidTexture_FluidDirWinsOverBlock(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/textures/fluid/steam_still.png")
	writeFixture(t, dir, "mod/textures/block/steam_still.png")

	url, _ := ResolveFluidTexture(dir, "mod", "steam")
	if want := "/assets/mod/textures/fluid/steam_still.png"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveMachineTexture_ModelFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "minecraft/models/block/furnace.json")

	url, ok := ResolveMachineTexture(dir, "minecraft", "furnace")
	if !ok {
		t.Fatal("expected ok=true for a machine with a real block model")
	}
	want := "/assets/render/minecraft/block/furnace.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveMachineTexture_BlockStateFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/blockstates/some_machine.json")

	url, ok := ResolveMachineTexture(dir, "mod", "some_machine")
	if !ok {
		t.Fatal("expected ok=true for a machine backed only by a blockstate")
	}
	want := "/assets/render/mod/block/some_machine.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveMachineTexture_ModelWinsOverGeneratedIcon(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/models/block/thing.json")
	writeFixture(t, dir, "mod/textures/generated/machine_icons/thing_south.png")

	url, _ := ResolveMachineTexture(dir, "mod", "thing")
	if want := "/assets/render/mod/block/thing.png"; url != want {
		t.Errorf("url = %q, want the model render %q", url, want)
	}
}

func TestResolveMachineTexture_ItemModelFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/models/item/assembler.json")

	url, ok := ResolveMachineTexture(dir, "modern_industrialization", "assembler")
	if !ok {
		t.Fatal("expected ok=true for a machine with only an item model")
	}
	want := "/assets/render/modern_industrialization/item/assembler.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveMachineTexture_ItemModelElectricPrefixFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/models/item/electric_compressor.json")

	url, ok := ResolveMachineTexture(dir, "modern_industrialization", "compressor")
	if !ok {
		t.Fatal("expected ok=true via the electric_ prefix fallback")
	}
	want := "/assets/render/modern_industrialization/item/electric_compressor.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveMachineTexture_ItemModelWinsOverGeneratedIcon(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mod/models/item/thing.json")
	writeFixture(t, dir, "mod/textures/generated/machine_icons/thing_south.png")

	url, _ := ResolveMachineTexture(dir, "mod", "thing")
	if want := "/assets/render/mod/item/thing.png"; url != want {
		t.Errorf("url = %q, want the item model render %q", url, want)
	}
}

func TestResolveMachineTexture_GeneratedIconDirect(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/textures/generated/machine_icons/electric_blast_furnace_south.png")

	url, ok := ResolveMachineTexture(dir, "modern_industrialization", "electric_blast_furnace")
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "/assets/modern_industrialization/textures/generated/machine_icons/electric_blast_furnace_south.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveMachineTexture_GeneratedIconElectricPrefixFallback(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "modern_industrialization/textures/generated/machine_icons/electric_compressor_south.png")

	url, ok := ResolveMachineTexture(dir, "modern_industrialization", "compressor")
	if !ok {
		t.Fatal("expected ok=true via the electric_ prefix fallback")
	}
	want := "/assets/modern_industrialization/textures/generated/machine_icons/electric_compressor_south.png"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestResolveMachineTexture_Missing(t *testing.T) {
	dir := t.TempDir()

	_, ok := ResolveMachineTexture(dir, "modern_industrialization", "nonexistent")
	if ok {
		t.Fatal("expected ok=false for a machine with no model and no generated icon")
	}
}

func TestResolveMachineTexture_RejectsEscapingSegments(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "secret.png")

	for _, tc := range []struct{ modID, machineID string }{
		{"..", "secret"},
		{"modern_industrialization", ".."},
		{"../modern_industrialization", "compressor"},
		{"modern_industrialization", "../secret"},
		{"", "compressor"},
		{"modern_industrialization", ""},
	} {
		if _, ok := ResolveMachineTexture(dir, tc.modID, tc.machineID); ok {
			t.Errorf("ResolveMachineTexture(%q, %q) = ok, want rejected", tc.modID, tc.machineID)
		}
	}
}

func writeText(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// A classic flat item model — parent item/generated, sprite in layer0 — is not a shape the
// renderer can draw; the item is the sprite itself.
func TestResolveItemTexture_GeneratedModelIsItsSprite(t *testing.T) {
	dir := t.TempDir()
	writeText(t, dir, "ironfurnaces/models/item/augment_speed.json",
		`{"parent": "item/generated", "textures": {"layer0": "ironfurnaces:item/augment_speed"}}`)
	writeFixture(t, dir, "ironfurnaces/textures/item/augment_speed.png")

	url, ok := ResolveItemTexture(dir, "ironfurnaces", "augment_speed")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "/assets/ironfurnaces/textures/item/augment_speed.png"; url != want {
		t.Errorf("url = %q, want the layer0 sprite %q", url, want)
	}
}

// The generated parent may sit further up the chain, behind a mod's own base model, and be
// spelled with or without the minecraft: namespace.
func TestResolveItemTexture_GeneratedModelBehindParentChain(t *testing.T) {
	dir := t.TempDir()
	writeText(t, dir, "mod/models/item/tool_base.json",
		`{"parent": "minecraft:item/handheld"}`)
	writeText(t, dir, "mod/models/item/wrench.json",
		`{"parent": "mod:item/tool_base", "textures": {"layer0": "mod:item/tools/wrench"}}`)
	writeFixture(t, dir, "mod/textures/item/tools/wrench.png")

	url, ok := ResolveItemTexture(dir, "mod", "wrench")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "/assets/mod/textures/item/tools/wrench.png"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

// A model whose chain leads to a block shape still goes to the renderer.
func TestResolveItemTexture_BlockModelStillRenders(t *testing.T) {
	dir := t.TempDir()
	writeText(t, dir, "ironfurnaces/models/item/iron_furnace.json",
		`{"parent": "ironfurnaces:block/iron_furnace"}`)
	writeText(t, dir, "ironfurnaces/models/block/iron_furnace.json",
		`{"parent": "minecraft:block/orientable", "textures": {"front": "ironfurnaces:block/iron_furnace_front"}}`)

	url, _ := ResolveItemTexture(dir, "ironfurnaces", "iron_furnace")
	if want := "/assets/render/ironfurnaces/item/iron_furnace.png"; url != want {
		t.Errorf("url = %q, want the model render %q", url, want)
	}
}

// A generated model whose sprite is missing on disk has nothing better than the renderer to
// offer.
func TestResolveItemTexture_GeneratedModelWithoutSpriteStillRenders(t *testing.T) {
	dir := t.TempDir()
	writeText(t, dir, "mod/models/item/ghost.json",
		`{"parent": "item/generated", "textures": {"layer0": "mod:item/ghost"}}`)

	url, _ := ResolveItemTexture(dir, "mod", "ghost")
	if want := "/assets/render/mod/item/ghost.png"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}
