package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
)

func TestAttachItemTextures(t *testing.T) {
	dir := t.TempDir()
	texDir := filepath.Join(dir, "modern_industrialization", "textures", "item")
	if err := os.MkdirAll(texDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(texDir, "motor.png"), []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	items := []*model.Item{
		{ModID: "modern_industrialization", ItemID: "motor"},
		{ModID: "modern_industrialization", ItemID: "no_texture"},
	}
	attachItemTextures(items, dir)

	if items[0].TextureURL == nil || *items[0].TextureURL != "/assets/modern_industrialization/textures/item/motor.png" {
		t.Errorf("motor: TextureURL = %v, want populated", items[0].TextureURL)
	}
	if items[1].TextureURL != nil {
		t.Errorf("no_texture: TextureURL = %v, want nil", *items[1].TextureURL)
	}
}

func TestAttachFluidTextures(t *testing.T) {
	dir := t.TempDir()
	texDir := filepath.Join(dir, "modern_industrialization", "textures", "fluid")
	if err := os.MkdirAll(texDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(texDir, "steam_still.png"), []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fluids := []*model.Fluid{
		{ModID: "modern_industrialization", FluidID: "steam"},
		{ModID: "modern_industrialization", FluidID: "no_texture"},
	}
	attachFluidTextures(fluids, dir)

	if fluids[0].TextureURL == nil || *fluids[0].TextureURL != "/assets/modern_industrialization/textures/fluid/steam_still.png" {
		t.Errorf("steam: TextureURL = %v, want populated", fluids[0].TextureURL)
	}
	if fluids[1].TextureURL != nil {
		t.Errorf("no_texture: TextureURL = %v, want nil", *fluids[1].TextureURL)
	}
}

func TestAttachItemTextures_AllRowsProcessed(t *testing.T) {
	dir := t.TempDir()
	texDir := filepath.Join(dir, "minecraft", "textures", "item")
	if err := os.MkdirAll(texDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, n := range []string{"potato.png", "brick.png"} {
		if err := os.WriteFile(filepath.Join(texDir, n), []byte("fake-png"), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", n, err)
		}
	}

	items := []*model.Item{
		{ModID: "minecraft", ItemID: "potato"},
		{ModID: "minecraft", ItemID: "missing"},
		{ModID: "minecraft", ItemID: "brick"},
	}
	attachItemTextures(items, dir)

	if items[0].TextureURL == nil {
		t.Error("potato: want populated TextureURL")
	}
	if items[1].TextureURL != nil {
		t.Errorf("missing: want nil, got %q", *items[1].TextureURL)
	}
	if items[2].TextureURL == nil {
		t.Error("brick: want populated TextureURL — a miss must not stop later rows")
	}
}
