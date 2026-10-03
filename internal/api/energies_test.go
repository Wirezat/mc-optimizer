package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
)

func TestListEnergiesHandler_ReturnsFormsWithFactor(t *testing.T) {
	d := deleteTestDB(t)
	ctx := context.Background()
	if _, err := d.Pool.Exec(ctx, `INSERT INTO mods (mod_id, name) VALUES ('api-energy', 'api-energy')`); err != nil {
		t.Fatalf("seed mod: %v", err)
	}
	t.Cleanup(func() { _, _ = d.Pool.Exec(context.Background(), `DELETE FROM mods WHERE mod_id = 'api-energy'`) })
	if err := d.ReplaceEnergies(ctx, "api-energy", []model.EnergyDef{
		{ModID: "api-energy", EnergyID: "eu", Symbol: "EU", FePerUnit: resource.NewRational(10, 1)},
	}); err != nil {
		t.Fatalf("ReplaceEnergies: %v", err)
	}

	rec := httptest.NewRecorder()
	ListEnergiesHandler(d)(rec, httptest.NewRequest(http.MethodGet, "/api/energies?all=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		ModID     string `json:"mod_id"`
		EnergyID  string `json:"energy_id"`
		Symbol    string `json:"symbol"`
		Name      string `json:"name"`
		FePerUnit struct {
			Num int64 `json:"num"`
			Den int64 `json:"den"`
		} `json:"fe_per_unit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, e := range got {
		if e.ModID == "api-energy" {
			if e.EnergyID != "eu" || e.Symbol != "EU" || e.Name != "EU" || e.FePerUnit.Num != 10 || e.FePerUnit.Den != 1 {
				t.Errorf("energy = %+v", e)
			}
			return
		}
	}
	t.Errorf("api-energy:eu missing from %s", rec.Body.String())
}
