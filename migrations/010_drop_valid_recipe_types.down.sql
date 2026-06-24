CREATE TABLE IF NOT EXISTS valid_recipe_types (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pattern          TEXT NOT NULL UNIQUE,
    is_regex         BOOLEAN NOT NULL DEFAULT FALSE,
    target_mod_id    TEXT,
    target_machine_id TEXT
);
