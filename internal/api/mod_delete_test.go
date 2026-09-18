package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/db"
)

func deleteTestDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	d, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

func deleteModRequest(t *testing.T, d *db.DB, modID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/mods/"+modID, nil)
	req.SetPathValue("mod_id", modID)
	rec := httptest.NewRecorder()
	DeleteModHandler(d)(rec, req)
	return rec
}

func TestDeleteModHandlerUnknownModIs404(t *testing.T) {
	d := deleteTestDB(t)
	if got := deleteModRequest(t, d, "api-delete-missing").Code; got != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", got)
	}
}

func TestDeleteModHandlerUnusedModIs204(t *testing.T) {
	d := deleteTestDB(t)
	ctx := context.Background()
	if _, err := d.Pool.Exec(ctx, `INSERT INTO mods (mod_id, name) VALUES ($1, $1)`, "api-delete-free"); err != nil {
		t.Fatalf("seed mod: %v", err)
	}
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM mods WHERE mod_id = 'api-delete-free'`)
	})

	if got := deleteModRequest(t, d, "api-delete-free").Code; got != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", got)
	}
}

// A mod another mod builds on answers 409 and names the blocker, so the admin knows what to
// clear before retrying.
func TestDeleteModHandlerUsedModIs409WithBlockers(t *testing.T) {
	d := deleteTestDB(t)
	ctx := context.Background()
	for _, m := range []string{"api-delete-base", "api-delete-dep"} {
		if _, err := d.Pool.Exec(ctx, `INSERT INTO mods (mod_id, name) VALUES ($1, $1)`, m); err != nil {
			t.Fatalf("seed mod %s: %v", m, err)
		}
	}
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(),
			`DELETE FROM mods WHERE mod_id IN ('api-delete-base','api-delete-dep')`)
	})
	if _, err := d.Pool.Exec(ctx,
		`INSERT INTO machine_types (mod_id, machine_id, name) VALUES ('api-delete-base','oven','oven')`); err != nil {
		t.Fatalf("seed machine type: %v", err)
	}
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO recipes (machine_mod_id, machine_id, source_mod_id, duration_ticks)
		VALUES ('api-delete-base','oven','api-delete-dep',1)
	`); err != nil {
		t.Fatalf("seed recipe: %v", err)
	}

	rec := deleteModRequest(t, d, "api-delete-base")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var body struct {
		Error    string `json:"error"`
		Blockers []struct {
			Kind   string   `json:"kind"`
			Count  int      `json:"count"`
			Sample []string `json:"sample"`
		} `json:"blockers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "MOD_IN_USE" {
		t.Fatalf("error = %q, want MOD_IN_USE", body.Error)
	}
	if len(body.Blockers) == 0 || body.Blockers[0].Kind != "dependent_mod" {
		t.Fatalf("blockers = %+v, want a dependent_mod entry", body.Blockers)
	}
	if body.Blockers[0].Count != 1 || len(body.Blockers[0].Sample) != 1 {
		t.Fatalf("blocker = %+v, want count 1 with one sample", body.Blockers[0])
	}

	var n int
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*) FROM mods WHERE mod_id = 'api-delete-base'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("mod was deleted despite the conflict")
	}
}
