-- 002_maschinen.up.sql
-- Maschinen, Upgrade-Tiers, Rezepte und alle Input/Output-Tabellen

CREATE TABLE machine_types (
    mod_id           TEXT      NOT NULL REFERENCES mods(mod_id),
    machine_id       TEXT      NOT NULL,
    name             TEXT      NOT NULL,
    base_eu_per_tick BIGINT,
    max_eu_per_tick  BIGINT,
    max_slots        SMALLINT,
    energy_type      TEXT,
    PRIMARY KEY (mod_id, machine_id)
);

CREATE TABLE upgrade_tiers (
    id               UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    mod_id           TEXT    NOT NULL REFERENCES mods(mod_id),
    name             TEXT    NOT NULL,
    eu_bonus_per_slot BIGINT,
    item_ref         TEXT
);

CREATE TABLE recipes (
    id              UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_mod_id  TEXT    NOT NULL,
    machine_id      TEXT    NOT NULL,
    duration_ticks  INT     NOT NULL,
    total_eu        BIGINT,
    priority        INT     NOT NULL DEFAULT 0,
    FOREIGN KEY (machine_mod_id, machine_id) REFERENCES machine_types(mod_id, machine_id)
);

CREATE TABLE recipe_item_inputs (
    id              UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID    NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    item_mod_id     TEXT,
    item_id         TEXT,
    tag_id          UUID    REFERENCES tags(id),
    amount_num      INT     NOT NULL,
    amount_den      INT     NOT NULL,
    probability_num INT     NOT NULL DEFAULT 1,
    probability_den INT     NOT NULL DEFAULT 1,
    -- Entweder item-basiert oder tag-basiert, nie beides
    CONSTRAINT chk_item_or_tag CHECK (
        (item_mod_id IS NOT NULL AND item_id IS NOT NULL AND tag_id IS NULL) OR
        (item_mod_id IS NULL AND item_id IS NULL AND tag_id IS NOT NULL)
    )
);

CREATE TABLE recipe_item_outputs (
    id              UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID    NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    item_mod_id     TEXT    NOT NULL,
    item_id         TEXT    NOT NULL,
    amount_num      INT     NOT NULL,
    amount_den      INT     NOT NULL,
    probability_num INT     NOT NULL DEFAULT 1,
    probability_den INT     NOT NULL DEFAULT 1,
    FOREIGN KEY (item_mod_id, item_id) REFERENCES items(mod_id, item_id)
);

CREATE TABLE recipe_fluid_inputs (
    id              UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID    NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    fluid_mod_id    TEXT    NOT NULL,
    fluid_id        TEXT    NOT NULL,
    amount_mb       BIGINT  NOT NULL,
    probability_num INT     NOT NULL DEFAULT 1,
    probability_den INT     NOT NULL DEFAULT 1,
    FOREIGN KEY (fluid_mod_id, fluid_id) REFERENCES fluids(mod_id, fluid_id)
);

CREATE TABLE recipe_fluid_outputs (
    id              UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID    NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    fluid_mod_id    TEXT    NOT NULL,
    fluid_id        TEXT    NOT NULL,
    amount_mb       BIGINT  NOT NULL,
    probability_num INT     NOT NULL DEFAULT 1,
    probability_den INT     NOT NULL DEFAULT 1,
    FOREIGN KEY (fluid_mod_id, fluid_id) REFERENCES fluids(mod_id, fluid_id)
);