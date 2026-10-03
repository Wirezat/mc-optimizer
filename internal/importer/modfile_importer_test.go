package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ModRecipeToNormalized must carry ModData through unchanged — it is the only place a
// recipe's mod-specific fields cross from the parsed modfile into what actually reaches the
// database and, from there, a plugin.
func TestModRecipeToNormalized_CarriesModData(t *testing.T) {
	r := model.ModRecipeDef{
		MachineModID:  "testmod",
		MachineID:     "iron_furnace",
		DurationTicks: 100,
		ModData:       map[string]any{"energy_per_tick": 16},
	}
	norm := ModRecipeToNormalized(r, "testmod")
	if got, ok := norm.ModData["energy_per_tick"]; !ok || got != 16 {
		t.Errorf("norm.ModData[energy_per_tick] = %#v (ok=%v), want 16", got, ok)
	}
}

// fakeImporterDB is a no-op ImporterDB for exercising RunModFile's control flow without a
// real database.
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
func (fakeImporterDB) UpsertMod(ctx context.Context, m model.ModDef) error { return nil }
func (fakeImporterDB) UpsertFluids(ctx context.Context, modID string, fluidIDs []string) error {
	return nil
}
func (fakeImporterDB) ReplaceEnergies(ctx context.Context, modID string, defs []model.EnergyDef) error {
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
func (fakeImporterDB) UpsertDirectTagMembers(ctx context.Context, sourceModID, kind, tagName string, members []string) (int, error) {
	return 0, nil
}
func (fakeImporterDB) UpsertModPlugin(ctx context.Context, p db.ModPlugin) error { return nil }

// recordingImporterDB wraps fakeImporterDB to count calls.
type recordingImporterDB struct {
	fakeImporterDB
	upsertModCalls    int
	upsertPluginCalls []db.ModPlugin
}

func (r *recordingImporterDB) UpsertMod(ctx context.Context, m model.ModDef) error {
	r.upsertModCalls++
	return nil
}

func (r *recordingImporterDB) UpsertModPlugin(ctx context.Context, p db.ModPlugin) error {
	r.upsertPluginCalls = append(r.upsertPluginCalls, p)
	return nil
}

// buildFixtureZipWithPlugin writes a minimal mod ZIP with a plugin/ subtree (plugin.yml +
// plugin.js) and returns its path.
func buildFixtureZipWithPlugin(t *testing.T, pluginYML, pluginJS string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustWrite := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	mustWrite("mod.yml", "mod_id: test_mod\n")
	mustWrite("plugin/plugin.yml", pluginYML)
	mustWrite("plugin/plugin.js", pluginJS)
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test_mod.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip fixture: %v", err)
	}
	return path
}

// A valid bundled plugin must be recorded with the fields from its plugin.yml, its full
// plugin.js source, and the importer's UploadedBy.
func TestRunModFile_InstallsBundledPlugin(t *testing.T) {
	zipPath := buildFixtureZipWithPlugin(t,
		"display_name: Test Plugin\nversion: 1.0.0\napi_version: 1\n",
		"var plugin = { api_version: 1, evaluate: function () { return [] } }",
	)
	rdb := &recordingImporterDB{}
	imp := New(rdb, t.TempDir())
	imp.UploadedBy = "tester"

	res, err := imp.RunModFile(context.Background(), zipPath)
	if err != nil {
		t.Fatalf("RunModFile: %v", err)
	}
	if !res.PluginInstalled {
		t.Error("PluginInstalled = false, want true")
	}
	if len(rdb.upsertPluginCalls) != 1 {
		t.Fatalf("UpsertModPlugin called %d times, want 1", len(rdb.upsertPluginCalls))
	}
	p := rdb.upsertPluginCalls[0]
	if p.ModID != "test_mod" || p.DisplayName != "Test Plugin" || p.Version != "1.0.0" || p.APIVersion != 1 {
		t.Errorf("got %+v, want test_mod / Test Plugin / 1.0.0 / 1", p)
	}
	if p.UploadedBy != "tester" {
		t.Errorf("UploadedBy = %q, want %q", p.UploadedBy, "tester")
	}
}

// A broken bundled plugin.js must fail the whole import before any DB write happens — a
// plugin that only fails at record time would already have left a half-imported mod behind
// it.
func TestRunModFile_InvalidPluginJSFailsBeforeAnyDBWrite(t *testing.T) {
	zipPath := buildFixtureZipWithPlugin(t,
		"display_name: Test Plugin\nversion: 1.0.0\napi_version: 1\n",
		"var plugin = { api_version: 1 }", // missing evaluate
	)
	rdb := &recordingImporterDB{}
	imp := New(rdb, t.TempDir())

	if _, err := imp.RunModFile(context.Background(), zipPath); err == nil {
		t.Fatal("want error for invalid plugin.js, got nil")
	}
	if rdb.upsertModCalls != 0 {
		t.Errorf("UpsertMod called %d times, want 0 — a broken plugin must not leave a half-imported mod behind", rdb.upsertModCalls)
	}
	if len(rdb.upsertPluginCalls) != 0 {
		t.Errorf("UpsertModPlugin called %d times, want 0", len(rdb.upsertPluginCalls))
	}
}

// buildFixtureZip writes a minimal mod ZIP (mod.yml + one assets/ texture entry) to a temp
// file and returns its path.
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

// A zip entry names its own destination path.
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
