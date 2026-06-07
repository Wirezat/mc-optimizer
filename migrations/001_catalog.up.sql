-- 001_catalog.up.sql
-- Catalog / seed data tables. No per-user ownership — admins write, all users read.

CREATE TABLE mods (
    mod_id      TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    energy_type TEXT NOT NULL CHECK (energy_type IN ('EU', 'FE', 'SU', 'NONE', 'CUSTOM'))
);

CREATE TABLE items (
    mod_id    TEXT     NOT NULL REFERENCES mods(mod_id),
    item_id   TEXT     NOT NULL,
    name      TEXT     NOT NULL,
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

CREATE TABLE machine_types (
    mod_id           TEXT     NOT NULL REFERENCES mods(mod_id),
    machine_id       TEXT     NOT NULL,
    name             TEXT     NOT NULL,
    base_eu_per_tick BIGINT,
    max_eu_per_tick  BIGINT,
    max_slots        SMALLINT,
    energy_type      TEXT,
    PRIMARY KEY (mod_id, machine_id)
);

CREATE TABLE upgrade_tiers (
    id                UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    mod_id            TEXT   NOT NULL REFERENCES mods(mod_id),
    name              TEXT   NOT NULL,
    eu_bonus_per_slot BIGINT,
    item_ref          TEXT
);

CREATE TABLE recipes (
    id              UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_mod_id  TEXT    NOT NULL,
    machine_id      TEXT    NOT NULL,
    name            TEXT,
    duration_ticks  INT     NOT NULL,
    eu_per_tick     BIGINT,
    total_eu        BIGINT,
    content_hash    TEXT,
    priority        INT     NOT NULL DEFAULT 0,
    FOREIGN KEY (machine_mod_id, machine_id) REFERENCES machine_types(mod_id, machine_id)
);

CREATE UNIQUE INDEX recipes_content_hash_idx ON recipes (content_hash) WHERE content_hash IS NOT NULL;

CREATE TABLE recipe_item_inputs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    item_mod_id     TEXT,
    item_id         TEXT,
    tag_id          UUID REFERENCES tags(id),
    amount_num      INT  NOT NULL,
    amount_den      INT  NOT NULL,
    probability_num INT  NOT NULL DEFAULT 1,
    probability_den INT  NOT NULL DEFAULT 1,
    CONSTRAINT chk_item_or_tag CHECK (
        (item_mod_id IS NOT NULL AND item_id IS NOT NULL AND tag_id IS NULL) OR
        (item_mod_id IS NULL     AND item_id IS NULL     AND tag_id IS NOT NULL)
    )
);

CREATE TABLE recipe_item_outputs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    item_mod_id     TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    amount_num      INT  NOT NULL,
    amount_den      INT  NOT NULL,
    probability_num INT  NOT NULL DEFAULT 1,
    probability_den INT  NOT NULL DEFAULT 1,
    FOREIGN KEY (item_mod_id, item_id) REFERENCES items(mod_id, item_id)
);

CREATE TABLE recipe_fluid_inputs (
    id              UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID   NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    fluid_mod_id    TEXT   NOT NULL,
    fluid_id        TEXT   NOT NULL,
    amount_mb       BIGINT NOT NULL,
    probability_num INT    NOT NULL DEFAULT 1,
    probability_den INT    NOT NULL DEFAULT 1,
    FOREIGN KEY (fluid_mod_id, fluid_id) REFERENCES fluids(mod_id, fluid_id)
);

CREATE TABLE recipe_fluid_outputs (
    id              UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID   NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    fluid_mod_id    TEXT   NOT NULL,
    fluid_id        TEXT   NOT NULL,
    amount_mb       BIGINT NOT NULL,
    probability_num INT    NOT NULL DEFAULT 1,
    probability_den INT    NOT NULL DEFAULT 1,
    FOREIGN KEY (fluid_mod_id, fluid_id) REFERENCES fluids(mod_id, fluid_id)
);

CREATE TABLE valid_recipe_types (
    id                UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    pattern           TEXT    NOT NULL UNIQUE,
    is_regex          BOOLEAN NOT NULL DEFAULT FALSE,
    target_mod_id     TEXT,
    target_machine_id TEXT,
    FOREIGN KEY (target_mod_id, target_machine_id)
        REFERENCES machine_types(mod_id, machine_id) ON DELETE SET NULL
);

-- Machine A implements machine B: A can run all of B's recipes.
-- Allows a modded machine to act as a drop-in for another without cloning recipes.
CREATE TABLE machine_interfaces (
    machine_mod_id  TEXT NOT NULL,
    machine_id      TEXT NOT NULL,
    base_mod_id     TEXT NOT NULL,
    base_machine_id TEXT NOT NULL,
    PRIMARY KEY (machine_mod_id, machine_id, base_mod_id, base_machine_id),
    FOREIGN KEY (machine_mod_id, machine_id)
        REFERENCES machine_types(mod_id, machine_id) ON DELETE CASCADE,
    FOREIGN KEY (base_mod_id, base_machine_id)
        REFERENCES machine_types(mod_id, machine_id) ON DELETE CASCADE
);

-- MI furnace implements vanilla furnace: runs all minecraft:smelting recipes.
INSERT INTO machine_interfaces (machine_mod_id, machine_id, base_mod_id, base_machine_id)
VALUES ('modern_industrialization', 'furnace', 'minecraft', 'furnace');
