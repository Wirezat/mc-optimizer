package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GetSetting returns the value for a key from admin_settings. Returns ErrNotFound if the
// key does not exist.
func (d *DB) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := d.Pool.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

// SetSetting upserts a key-value pair in admin_settings.
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO admin_settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	return err
}

// RegistrationEnabled returns true if registration is allowed.
func (d *DB) RegistrationEnabled(ctx context.Context) (bool, error) {
	val, err := d.GetSetting(ctx, "registration_enabled")
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return val == "true", nil
}
