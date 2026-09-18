package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wirezat/production-optimizer/internal/plugins"
	"github.com/jackc/pgx/v5"
)

// VariantCacheHash maps everything evaluate() reads — plugin version, the machine's and the
// recipe's mod_data, and the save's config — to a cache key.
func VariantCacheHash(pluginVersion string, machineModData, recipeModData, cfg json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(pluginVersion))
	for _, part := range []json.RawMessage{machineModData, recipeModData, cfg} {
		h.Write([]byte{0})
		h.Write(canonicalJSON(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON returns a key-order-independent encoding of raw, decoding numbers as
// json.Number so large integers survive exactly.
func canonicalJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	if dec.More() {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// GetVariants reads a cache entry. ErrNotFound means a cache miss.
func (d *DB) GetVariants(ctx context.Context, modID, machineID, recipeID, configHash string) ([]plugins.Variant, error) {
	var raw []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT variants FROM machine_recipe_variants
		WHERE mod_id = $1 AND machine_id = $2 AND recipe_id = $3 AND config_hash = $4
	`, modID, machineID, recipeID, configHash).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var vs []plugins.Variant
	if err := json.Unmarshal(raw, &vs); err != nil {
		return nil, fmt.Errorf("db: decode cached variants: %w", err)
	}
	return vs, nil
}

// GetAnyBaseVariantCosts returns the operating cost of a cached base variant (no installed
// items) for a machine, regardless of which recipe or plugin config produced it — the
// catalog has no save context to pick one precisely. ErrNotFound means no base variant at
// all has been computed for this machine yet.
func (d *DB) GetAnyBaseVariantCosts(ctx context.Context, modID, machineID string) ([]plugins.Cost, error) {
	var raw []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT v.variant -> 'costs'
		FROM machine_recipe_variants m,
		     LATERAL jsonb_array_elements(m.variants) AS v(variant)
		WHERE m.mod_id = $1 AND m.machine_id = $2
		  AND jsonb_array_length(COALESCE(NULLIF(v.variant -> 'items', 'null'::jsonb), '[]'::jsonb)) = 0
		ORDER BY (jsonb_array_length(COALESCE(NULLIF(v.variant -> 'costs', 'null'::jsonb), '[]'::jsonb)) = 0),
		         m.recipe_id, m.config_hash
		LIMIT 1
	`, modID, machineID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var costs []plugins.Cost
	if err := json.Unmarshal(raw, &costs); err != nil {
		return nil, fmt.Errorf("db: decode cached variant costs: %w", err)
	}
	return costs, nil
}

// PutVariants stores a cache entry, replacing any existing entry under the same key.
func (d *DB) PutVariants(ctx context.Context, modID, machineID, recipeID, configHash string, vs []plugins.Variant) error {
	raw, err := json.Marshal(vs)
	if err != nil {
		return fmt.Errorf("db: encode variants: %w", err)
	}
	_, err = d.Pool.Exec(ctx, `
		INSERT INTO machine_recipe_variants (mod_id, machine_id, recipe_id, config_hash, variants)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (mod_id, machine_id, recipe_id, config_hash)
		DO UPDATE SET variants = EXCLUDED.variants, computed_at = now()
	`, modID, machineID, recipeID, configHash, raw)
	return err
}
