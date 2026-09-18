package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/jackc/pgx/v5"
)

// maxBlockerSamples caps how many names a blocker carries.
const maxBlockerSamples = 3

// ErrModInUse reports a mod that cannot be deleted, with one entry per reason.
type ErrModInUse struct {
	ModID    string
	Blockers []model.ModBlocker
}

func (e *ErrModInUse) Error() string {
	kinds := make([]string, 0, len(e.Blockers))
	for _, b := range e.Blockers {
		kinds = append(kinds, fmt.Sprintf("%s=%d", b.Kind, b.Count))
	}
	return fmt.Sprintf("db: mod %s is in use (%s)", e.ModID, strings.Join(kinds, ", "))
}

// querier is the subset of pgx both a pool and a transaction satisfy.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// blockerQueries pairs a blocker kind with the SQL that finds it. Each query takes the mod
// id as $1 and returns one row per offending entity: a display name and the total count.
var blockerQueries = []struct {
	kind string
	sql  string
}{
	{"production_line", `
		WITH hit AS (
			SELECT DISTINCT pl.id, pl.target_item_id, f.name AS factory_name
			FROM production_lines pl
			JOIN factories f ON f.id = pl.factory_id
			LEFT JOIN machine_groups mg ON mg.pl_id = pl.id
			LEFT JOIN recipes r ON r.id = mg.recipe_id
			LEFT JOIN pl_io io ON io.pl_id = pl.id
			WHERE pl.target_mod_id = $1
			   OR mg.machine_mod_id = $1
			   OR r.source_mod_id = $1
			   OR r.machine_mod_id = $1
			   OR io.mod_id = $1
		)
		SELECT target_item_id || ' (' || factory_name || ')', (SELECT count(*) FROM hit)
		FROM hit ORDER BY 1`},

	{"factory_source", `
		WITH hit AS (
			SELECT DISTINCT f.id, f.name
			FROM factories f
			LEFT JOIN factory_source_inputs i ON i.factory_id = f.id
			LEFT JOIN factory_source_outputs o ON o.factory_id = f.id
			WHERE i.mod_id = $1 OR o.mod_id = $1
		)
		SELECT name, (SELECT count(*) FROM hit) FROM hit ORDER BY 1`},

	{"save", `
		WITH hit AS (
			SELECT DISTINCT s.id, s.name
			FROM saves s
			LEFT JOIN save_active_mods a ON a.save_id = s.id
			LEFT JOIN save_mod_config_defaults c ON c.save_id = s.id
			WHERE a.mod_id = $1 OR c.mod_id = $1
		)
		SELECT name, (SELECT count(*) FROM hit) FROM hit ORDER BY 1`},

	{"dependent_mod", `
		WITH hit AS (
			SELECT DISTINCT source_mod_id AS mod_id FROM recipes
			WHERE machine_mod_id = $1 AND source_mod_id <> $1
			UNION
			SELECT DISTINCT machine_mod_id FROM machine_interfaces
			WHERE base_mod_id = $1 AND machine_mod_id <> $1
		)
		SELECT mod_id, (SELECT count(*) FROM hit) FROM hit ORDER BY 1`},
}

// ModUsage reports why modID cannot be deleted. An empty result means the mod is unused and
// safe to delete. Tag members and villager trades are left out: they cascade away without
// costing another mod a machine or a recipe.
func (d *DB) ModUsage(ctx context.Context, modID string) ([]model.ModBlocker, error) {
	return modUsage(ctx, d.Pool, modID)
}

func modUsage(ctx context.Context, q querier, modID string) ([]model.ModBlocker, error) {
	var out []model.ModBlocker
	for _, bq := range blockerQueries {
		rows, err := q.Query(ctx, bq.sql, modID)
		if err != nil {
			return nil, fmt.Errorf("db: mod usage %s: %w", bq.kind, err)
		}
		var sample []string
		total := 0
		for rows.Next() {
			var name string
			if err := rows.Scan(&name, &total); err != nil {
				rows.Close()
				return nil, fmt.Errorf("db: mod usage %s: scan: %w", bq.kind, err)
			}
			if len(sample) < maxBlockerSamples {
				sample = append(sample, name)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("db: mod usage %s: %w", bq.kind, err)
		}
		if total > 0 {
			out = append(out, model.ModBlocker{Kind: bq.kind, Count: total, Sample: sample})
		}
	}
	return out, nil
}

// DeleteMod removes a mod and everything that cascades from it. Returns ErrNotFound for an
// unknown mod and *ErrModInUse when anything still references it.
func (d *DB) DeleteMod(ctx context.Context, modID string) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: delete mod: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Locking the row holds the usage check and the delete together against a second
	// delete of the same mod.
	var found string
	if err := tx.QueryRow(ctx,
		`SELECT mod_id FROM mods WHERE mod_id = $1 FOR UPDATE`, modID).Scan(&found); err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return fmt.Errorf("db: delete mod: lock: %w", err)
	}

	blockers, err := modUsage(ctx, tx, modID)
	if err != nil {
		return err
	}
	if len(blockers) > 0 {
		return &ErrModInUse{ModID: modID, Blockers: blockers}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM mods WHERE mod_id = $1`, modID); err != nil {
		return fmt.Errorf("db: delete mod: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: delete mod: commit: %w", err)
	}
	return nil
}
