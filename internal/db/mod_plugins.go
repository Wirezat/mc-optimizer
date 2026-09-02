package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ModPlugin is a mod's plugin as stored in the database, source included.
type ModPlugin struct {
	ModID       string    `json:"mod_id"`
	DisplayName string    `json:"display_name"`
	Version     string    `json:"version"`
	APIVersion  int       `json:"api_version"`
	Source      string    `json:"-"`
	HasWizard   bool      `json:"has_wizard"`
	UploadedAt  time.Time `json:"uploaded_at"`
	UploadedBy  string    `json:"uploaded_by,omitempty"`
}

const modPluginColumns = `mod_id, display_name, version, api_version, source, has_wizard, uploaded_at, uploaded_by`

// UpsertModPlugin inserts a mod's plugin or replaces it if one already exists.
func (d *DB) UpsertModPlugin(ctx context.Context, p ModPlugin) error {
	_, err := d.Pool.Exec(ctx, `
		INSERT INTO mod_plugins (mod_id, display_name, version, api_version, source, has_wizard, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7,''))
		ON CONFLICT (mod_id) DO UPDATE SET
		    display_name = EXCLUDED.display_name,
		    version      = EXCLUDED.version,
		    api_version  = EXCLUDED.api_version,
		    source       = EXCLUDED.source,
		    has_wizard   = EXCLUDED.has_wizard,
		    uploaded_at  = now(),
		    uploaded_by  = COALESCE(EXCLUDED.uploaded_by, mod_plugins.uploaded_by)
	`, p.ModID, p.DisplayName, p.Version, p.APIVersion, p.Source, p.HasWizard, p.UploadedBy)
	return err
}

// GetModPlugin fetches a mod's plugin by mod ID; returns ErrNotFound if none is stored.
func (d *DB) GetModPlugin(ctx context.Context, modID string) (*ModPlugin, error) {
	row := d.Pool.QueryRow(ctx, `SELECT `+modPluginColumns+` FROM mod_plugins WHERE mod_id = $1`, modID)
	p, err := scanModPlugin(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

// ListModPlugins fetches every installed plugin, ordered by mod ID.
func (d *DB) ListModPlugins(ctx context.Context) ([]*ModPlugin, error) {
	rows, err := d.Pool.Query(ctx, `SELECT `+modPluginColumns+` FROM mod_plugins ORDER BY mod_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ModPlugin
	for rows.Next() {
		p, err := scanModPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type scannable interface{ Scan(dest ...any) error }

func scanModPlugin(s scannable) (*ModPlugin, error) {
	var p ModPlugin
	var uploadedBy *string
	if err := s.Scan(&p.ModID, &p.DisplayName, &p.Version, &p.APIVersion,
		&p.Source, &p.HasWizard, &p.UploadedAt, &uploadedBy); err != nil {
		return nil, err
	}
	if uploadedBy != nil {
		p.UploadedBy = *uploadedBy
	}
	return &p, nil
}
