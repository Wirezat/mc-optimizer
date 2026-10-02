package db

import (
	"context"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

// ListResourceKinds returns every row of resource_kinds.
func (d *DB) ListResourceKinds(ctx context.Context) ([]resource.KindInfo, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT kind, key_prefix, base_unit, COALESCE(uom_system, ''), expand FROM resource_kinds`)
	if err != nil {
		return nil, fmt.Errorf("db: list resource kinds: %w", err)
	}
	defer rows.Close()
	var out []resource.KindInfo
	for rows.Next() {
		var k resource.KindInfo
		if err := rows.Scan(&k.Kind, &k.KeyPrefix, &k.BaseUnit, &k.UOMSystem, &k.Expand); err != nil {
			return nil, fmt.Errorf("db: list resource kinds: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
