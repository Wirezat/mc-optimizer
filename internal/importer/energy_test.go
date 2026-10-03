package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
)

func TestParseModFile_Energies(t *testing.T) {
	def, err := ParseModFile([]byte(`mod_id: emod
energies:
  - id: eu
    symbol: EU
    lang_key: text.emod.eu
    fe_per_unit: 10
  - id: emod:third
    symbol: TE
    fe_per_unit: 1/3
`))
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	want := []model.EnergyDef{
		{ModID: "emod", EnergyID: "eu", Symbol: "EU", LangKey: "text.emod.eu", FePerUnit: resource.NewRational(10, 1)},
		{ModID: "emod", EnergyID: "third", Symbol: "TE", FePerUnit: resource.NewRational(1, 3)},
	}
	if len(def.Energies) != len(want) {
		t.Fatalf("energies = %+v, want %+v", def.Energies, want)
	}
	for i := range want {
		if def.Energies[i] != want[i] {
			t.Errorf("energy %d = %+v, want %+v", i, def.Energies[i], want[i])
		}
	}
}

func TestParseModFile_EnergyExampleSentinelIsSkipped(t *testing.T) {
	def, err := ParseModFile([]byte("mod_id: emod\nenergies:\n  - id: _example_\n    symbol: _example_\n    fe_per_unit: 1\n"))
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if len(def.Energies) != 0 {
		t.Errorf("energies = %+v, want none", def.Energies)
	}
}

func TestParseModFile_RejectsBadEnergies(t *testing.T) {
	for name, body := range map[string]string{
		"zero factor":     "  - id: eu\n    symbol: EU\n    fe_per_unit: 0\n",
		"negative factor": "  - id: eu\n    symbol: EU\n    fe_per_unit: -10\n",
		"missing factor":  "  - id: eu\n    symbol: EU\n",
		"not a number":    "  - id: eu\n    symbol: EU\n    fe_per_unit: lots\n",
		"missing symbol":  "  - id: eu\n    fe_per_unit: 10\n",
		"missing id":      "  - symbol: EU\n    fe_per_unit: 10\n",
		"foreign mod":     "  - id: other:eu\n    symbol: EU\n    fe_per_unit: 10\n",
		"duplicate id":    "  - id: eu\n    symbol: EU\n    fe_per_unit: 10\n  - id: emod:eu\n    symbol: EU\n    fe_per_unit: 10\n",
	} {
		_, err := ParseModFile([]byte("mod_id: emod\nenergies:\n" + body))
		if err == nil {
			t.Errorf("%s: ParseModFile succeeded, want an error", name)
			continue
		}
		if !strings.Contains(err.Error(), "energ") {
			t.Errorf("%s: error %q does not name the energy", name, err)
		}
	}
}

func TestParseModFile_EnergyErrorNamesWhere(t *testing.T) {
	_, err := ParseModFile([]byte("mod_id: emod\nenergies:\n  - id: eu\n    symbol: EU\n    fe_per_unit: 10\n  - id: rf\n    symbol: RF\n    fe_per_unit: 0\n"))
	if err == nil {
		t.Fatal("ParseModFile succeeded, want an error")
	}
	for _, want := range []string{`energy "emod:rf"`, "line 6"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

type energyRecordingDB struct {
	fakeImporterDB
	calls map[string][]model.EnergyDef
}

func (r *energyRecordingDB) ReplaceEnergies(ctx context.Context, modID string, defs []model.EnergyDef) error {
	r.calls[modID] = defs
	return nil
}

func modZip(t *testing.T, modYML string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("mod.yml")
	if err != nil {
		t.Fatalf("create mod.yml: %v", err)
	}
	if _, err := w.Write([]byte(modYML)); err != nil {
		t.Fatalf("write mod.yml: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	path := filepath.Join(t.TempDir(), "mod.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	return path
}

func TestRunModFile_ReplacesEnergies(t *testing.T) {
	rdb := &energyRecordingDB{calls: map[string][]model.EnergyDef{}}
	res, err := New(rdb, t.TempDir()).RunModFile(context.Background(),
		modZip(t, "mod_id: emod\nenergies:\n  - id: eu\n    symbol: EU\n    fe_per_unit: 10\n"))
	if err != nil {
		t.Fatalf("RunModFile: %v", err)
	}
	got, called := rdb.calls["emod"]
	if !called || len(got) != 1 || got[0].EnergyID != "eu" {
		t.Fatalf("ReplaceEnergies(emod) = %+v (called %v), want eu", got, called)
	}
	if res.Energies != 1 {
		t.Errorf("res.Energies = %d, want 1", res.Energies)
	}
}

func TestRunModFile_WithoutEnergiesStillReplaces(t *testing.T) {
	rdb := &energyRecordingDB{calls: map[string][]model.EnergyDef{}}
	if _, err := New(rdb, t.TempDir()).RunModFile(context.Background(), modZip(t, "mod_id: emod\n")); err != nil {
		t.Fatalf("RunModFile: %v", err)
	}
	if got, called := rdb.calls["emod"]; !called || len(got) != 0 {
		t.Errorf("ReplaceEnergies(emod) = %+v (called %v), want an empty call", got, called)
	}
}
