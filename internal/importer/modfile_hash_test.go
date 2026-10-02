package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRealModfilesKeepTheirHashes(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer pool.Close()

	files, err := filepath.Glob("../../mod-sources/modfiles/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no modfiles found: %v", err)
	}
	total := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		def, err := ParseModFile(data)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for i, r := range def.Recipes {
			hash := ContentHash(ModRecipeToNormalized(r, def.ModID))
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipes WHERE content_hash = $1`, hash).Scan(&n); err != nil {
				t.Fatalf("query: %v", err)
			}
			if n != 1 {
				t.Errorf("%s recipe %d (%s:%s): hash %s not in the catalog", filepath.Base(f), i, r.MachineModID, r.MachineID, hash)
			}
			total++
		}
	}
	t.Logf("%d recipes checked", total)
}
