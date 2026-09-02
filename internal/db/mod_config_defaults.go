package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// GetSaveModConfigDefaults returns the per-mod seed configs of a save, keyed by
// mod id. A save that has none yields an empty map, which every plugin reads as
// its own defaults.
func (d *DB) GetSaveModConfigDefaults(ctx context.Context, saveID uuid.UUID) (map[string]json.RawMessage, error) {
	rows, err := d.Pool.Query(ctx,
		`SELECT mod_id, config FROM save_mod_config_defaults WHERE save_id = $1`, saveID)
	if err != nil {
		return nil, fmt.Errorf("db: get mod config defaults: %w", err)
	}
	defer rows.Close()

	out := map[string]json.RawMessage{}
	for rows.Next() {
		var modID string
		var cfg []byte
		if err := rows.Scan(&modID, &cfg); err != nil {
			return nil, fmt.Errorf("db: get mod config defaults: scan: %w", err)
		}
		out[modID] = json.RawMessage(cfg)
	}
	return out, rows.Err()
}

// SetSaveModConfigDefault stores one mod's seed config for a save, replacing any
// previous one. An empty config is stored as the empty object.
func (d *DB) SetSaveModConfigDefault(ctx context.Context, saveID uuid.UUID, modID string, cfg json.RawMessage) error {
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO save_mod_config_defaults (save_id, mod_id, config)
		VALUES ($1, $2, $3)
		ON CONFLICT (save_id, mod_id) DO UPDATE SET config = EXCLUDED.config
	`, saveID, modID, []byte(cfg))
	if err != nil {
		return fmt.Errorf("db: set mod config default %s: %w", modID, err)
	}
	return nil
}
