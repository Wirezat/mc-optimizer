-- 004_admin_settings.up.sql

CREATE TABLE admin_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Default: registration is open
INSERT INTO admin_settings (key, value) VALUES ('registration_enabled', 'true');
