package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// fakeImporterDB is a no-op ImporterDB for exercising RunModFile's control
// flow without a real database.
type fakeImporterDB struct{}

func (fakeImporterDB) ImportRecipe(ctx context.Context, rec model.NormalizedRecipe) (bool, error) {
	return true, nil
}
func (fakeImporterDB) UpsertTranslations(ctx context.Context, lang string, entries map[string]string) error {
	return nil
}
func (fakeImporterDB) BulkUpsertItems(ctx context.Context, modID string, items []model.ItemDef) error {
	return nil
}
func (fakeImporterDB) UpsertBlockDrops(ctx context.Context, drops []model.BlockDrop) error {
	return nil
}
func (fakeImporterDB) UpsertVillagerTrades(ctx context.Context, trades []model.VillagerTrade) error {
	return nil
}
func (fakeImporterDB) UpdateModMetadata(ctx context.Context, meta model.ModMetadata) error {
	return nil
}
func (fakeImporterDB) SetMIEnergyType(ctx context.Context, modIDs []string) error { return nil }
func (fakeImporterDB) SetMachinesUpgradable(ctx context.Context, modIDs []string) error {
	return nil
}
func (fakeImporterDB) UpsertMod(ctx context.Context, m model.ModDef) error { return nil }
func (fakeImporterDB) UpsertFluids(ctx context.Context, modID string, fluidIDs []string) error {
	return nil
}
func (fakeImporterDB) UpsertMachineType(ctx context.Context, m model.MachineTypeDef) error {
	return nil
}
func (fakeImporterDB) UpsertMachineSlots(ctx context.Context, slots []model.MachineSlotDef) error {
	return nil
}
func (fakeImporterDB) AddMachineInterface(ctx context.Context, modID, machineID, baseModID, baseMachineID string) error {
	return nil
}
func (fakeImporterDB) UpsertDirectTagMembers(ctx context.Context, tagName string, members []string) error {
	return nil
}
func (fakeImporterDB) UpsertUpgradeTier(ctx context.Context, modID, name string, euBonusPerSlot int64, itemRef string) error {
	return nil
}

// buildFixtureZip writes a minimal mod ZIP (mod.yml + one assets/ texture
// entry) to a temp file and returns its path.
func buildFixtureZip(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	modYML, err := zw.Create("mod.yml")
	if err != nil {
		t.Fatalf("create mod.yml entry: %v", err)
	}
	if _, err := modYML.Write([]byte("mod_id: test_mod\n")); err != nil {
		t.Fatalf("write mod.yml: %v", err)
	}

	texture, err := zw.Create("assets/test_mod/textures/item/motor.png")
	if err != nil {
		t.Fatalf("create texture entry: %v", err)
	}
	if _, err := texture.Write([]byte("fake-png")); err != nil {
		t.Fatalf("write texture: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	path := filepath.Join(t.TempDir(), "test_mod.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip fixture: %v", err)
	}
	return path
}

func TestRunModFile_ExtractsTextureWithoutDoubledAssetsPrefix(t *testing.T) {
	zipPath := buildFixtureZip(t)
	assetsDir := t.TempDir()

	imp := New(fakeImporterDB{}, assetsDir)
	if _, err := imp.RunModFile(context.Background(), zipPath); err != nil {
		t.Fatalf("RunModFile: %v", err)
	}

	want := filepath.Join(assetsDir, "test_mod", "textures", "item", "motor.png")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected texture at %s, stat error: %v", want, err)
	}

	doubled := filepath.Join(assetsDir, "assets", "test_mod", "textures", "item", "motor.png")
	if _, err := os.Stat(doubled); err == nil {
		t.Errorf("texture landed at doubled path %s, should not exist", doubled)
	}
}

// A zip entry names its own destination path. One crafted to climb out of the
// assets directory must be refused, not written wherever it points.
func TestUnderDir(t *testing.T) {
	for _, tc := range []struct {
		dir, path string
		want      bool
	}{
		{"assets", "assets/mod/textures/x.png", true},
		{"assets", "assets", true},
		{"assets", "assets/../outside.png", false},
		{"assets", "outside.png", false},
		{"assets", "assets/../../etc/passwd", false},
		{"/srv/assets", "/srv/assets/mod/a.png", true},
		{"/srv/assets", "/srv/other/a.png", false},
	} {
		if got := underDir(tc.dir, tc.path); got != tc.want {
			t.Errorf("underDir(%q, %q) = %v, want %v", tc.dir, tc.path, got, tc.want)
		}
	}
}
