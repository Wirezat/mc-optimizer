CREATE TABLE mods (
    mod_id      TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    energy_type TEXT NOT NULL CHECK (energy_type IN ('EU', 'FE', 'SU', 'NONE', 'CUSTOM')),
    created_by  UUID  -- FK → users (wird in Migration 003 nachgetragen)
);

CREATE TABLE items (
    mod_id    TEXT NOT NULL REFERENCES mods(mod_id),
    item_id   TEXT NOT NULL,
    name      TEXT NOT NULL,
    max_stack SMALLINT NOT NULL DEFAULT 64,
    PRIMARY KEY (mod_id, item_id)
);

CREATE TABLE fluids (
    mod_id   TEXT NOT NULL REFERENCES mods(mod_id),
    fluid_id TEXT NOT NULL,
    name     TEXT NOT NULL,
    PRIMARY KEY (mod_id, fluid_id)
);

CREATE TABLE tags (
    id   UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE tag_members (
    tag_id      UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    item_mod_id TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    PRIMARY KEY (tag_id, item_mod_id, item_id),
    FOREIGN KEY (item_mod_id, item_id) REFERENCES items(mod_id, item_id) ON DELETE CASCADE
);