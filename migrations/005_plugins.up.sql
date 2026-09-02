-- 005_plugins.up.sql

CREATE TABLE mod_plugins (
    mod_id       TEXT    PRIMARY KEY REFERENCES mods(mod_id) ON DELETE CASCADE,
    display_name TEXT    NOT NULL,
    version      TEXT    NOT NULL,
    api_version  INT     NOT NULL,
    -- The plugin's JS source itself, stored in the DB by design instead of the filesystem.
    source       TEXT    NOT NULL,
    has_wizard   BOOLEAN NOT NULL DEFAULT FALSE,
    uploaded_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    uploaded_by  TEXT
);

CREATE TABLE machine_recipe_variants (
    mod_id      TEXT NOT NULL,
    machine_id  TEXT NOT NULL,
    recipe_id   UUID NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    -- sha256 over (plugin version || canonical JSON of the config); a change
    -- lands on a different key instead of invalidating rows.
    config_hash TEXT NOT NULL,
    variants    JSONB NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (mod_id, machine_id, recipe_id, config_hash),
    FOREIGN KEY (mod_id, machine_id)
        REFERENCES machine_types(mod_id, machine_id) ON DELETE CASCADE
);

-- Each of these foreign keys sits at the third (recipe_id) resp. second
-- (mod_id) position of its table's primary key, so that index cannot serve a
-- lookup by the key alone and a DELETE on the referenced side would seq scan
-- the whole table. machine_recipe_variants is the fastest-growing table in the
-- project (machines x recipes x configs), and deleting a mod cascades into both.
CREATE INDEX machine_recipe_variants_recipe_idx ON machine_recipe_variants (recipe_id);

-- Seed for a fresh chain on this world, copied into machine_groups.mod_config
-- when a line is confirmed and never read again after that. Distinct from the
-- removed save_mod_configs, which was consulted at evaluation time and made a
-- later change reach lines that were already built.
CREATE TABLE save_mod_config_defaults (
    save_id UUID  NOT NULL REFERENCES saves(id) ON DELETE CASCADE,
    mod_id  TEXT  NOT NULL REFERENCES mods(mod_id) ON DELETE CASCADE,
    config  JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (save_id, mod_id)
);
