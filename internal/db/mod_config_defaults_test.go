package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestSetAndGetSaveModConfigDefaults(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	saveID := seedSave(t, d)
	seedMod(t, d, "defmod")

	if err := d.SetSaveModConfigDefault(ctx, saveID, "defmod", json.RawMessage(`{"tier":"steel"}`)); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := d.GetSaveModConfigDefaults(ctx, saveID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got["defmod"]) != `{"tier": "steel"}` && string(got["defmod"]) != `{"tier":"steel"}` {
		t.Errorf("config = %q, want the stored one", got["defmod"])
	}
}

// A second write replaces the first: the default is one value per mod, not a history.
func TestSetSaveModConfigDefaultReplaces(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	saveID := seedSave(t, d)
	seedMod(t, d, "defmod2")

	for _, cfg := range []string{`{"tier":"bronze"}`, `{"tier":"electric"}`} {
		if err := d.SetSaveModConfigDefault(ctx, saveID, "defmod2", json.RawMessage(cfg)); err != nil {
			t.Fatalf("set %s: %v", cfg, err)
		}
	}
	got, err := d.GetSaveModConfigDefaults(ctx, saveID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(got["defmod2"], &parsed); err != nil {
		t.Fatalf("unmarshal %q: %v", got["defmod2"], err)
	}
	if parsed["tier"] != "electric" {
		t.Errorf("tier = %q, want the second write to win", parsed["tier"])
	}
}

// A save with no defaults yields an empty map, which every plugin reads as its own
// defaults.
func TestGetSaveModConfigDefaultsEmpty(t *testing.T) {
	d := testDB(t)
	got, err := d.GetSaveModConfigDefaults(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries for an unknown save, want 0", len(got))
	}
}
