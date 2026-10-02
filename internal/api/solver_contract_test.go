package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postJSON(t *testing.T, h http.HandlerFunc, body string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if strings.Contains(rec.Body.String(), `"IsFluid"`) || strings.Contains(rec.Body.String(), `"ModID"`) || strings.Contains(rec.Body.String(), `"Num"`) {
		t.Errorf("response still has PascalCase keys: %.300s", rec.Body.String())
	}
	return out
}

func TestSolverAPIIsSnakeCase(t *testing.T) {
	d := deleteTestDB(t)
	var id string
	if err := d.Pool.QueryRow(context.Background(), `
		SELECT r.id::text FROM recipes r
		JOIN recipe_fluid_inputs f ON f.recipe_id = r.id JOIN tags t ON t.id = f.tag_id
		JOIN recipe_item_outputs o ON o.recipe_id = r.id
		WHERE t.name = 'c:honey' AND o.item_id = 'honey_bottle' LIMIT 1`).Scan(&id); err != nil {
		t.Skipf("no c:honey canning recipe in the catalog: %v", err)
	}
	target := `{"mod_id":"minecraft","id":"honey_bottle","kind":"item"}`

	disc := postJSON(t, DemoDiscoverHandler(d), `{"target":`+target+`,"recipe_overrides":{"minecraft:honey_bottle":"`+id+`"}}`)
	for _, k := range []string{"items", "tag_resolutions", "names", "machine_names", "machine_plugin_mods"} {
		if _, ok := disc[k]; !ok {
			t.Errorf("discover response lacks %q", k)
		}
	}

	sol := postJSON(t, DemoSolveHandler(d, 64, nil), `{"target":`+target+`,"target_rate":{"num":1,"den":20},"time_unit":"t","mode":"TARGET","recipe_overrides":{"minecraft:honey_bottle":"`+id+`"}}`)
	result, _ := sol["result"].(map[string]any)
	io, _ := result["io_profile"].(map[string]any)
	inputs, _ := io["inputs"].([]any)
	var honey map[string]any
	for _, in := range inputs {
		ref := in.(map[string]any)["ref"].(map[string]any)
		if ref["id"] == "honey" {
			honey = ref
		}
	}
	if honey == nil || honey["kind"] != "fluid" || honey["mod_id"] != "extended_industrialization" {
		t.Errorf("honey input ref = %v, want a fluid ref from extended_industrialization", honey)
	}
	if _, ok := result["actual_rate"].(map[string]any)["num"]; !ok {
		t.Errorf("actual_rate = %v, want {num, den}", result["actual_rate"])
	}
	if tr, _ := result["tag_resolutions"].(map[string]any); tr["fluid:#c:honey"] == nil {
		t.Errorf("tag_resolutions = %v, want fluid:#c:honey", tr)
	}
}
