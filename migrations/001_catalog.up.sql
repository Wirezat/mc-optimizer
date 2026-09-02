-- 001_catalog.up.sql
-- Catalog / seed data tables. No per-user ownership — admins write, all users read.

CREATE TABLE mods (
    mod_id        TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    description   TEXT,
    author        TEXT,
    license       TEXT,
    url_source    TEXT,
    url_modrinth  TEXT,
    url_wiki      TEXT,
    url_issues    TEXT,
    url_discord   TEXT,
    modrinth_slug TEXT
);

CREATE TABLE items (
    mod_id    TEXT     NOT NULL REFERENCES mods(mod_id) ON DELETE CASCADE,
    item_id   TEXT     NOT NULL,
    max_stack SMALLINT NOT NULL DEFAULT 64,
    PRIMARY KEY (mod_id, item_id)
);

CREATE TABLE fluids (
    mod_id   TEXT NOT NULL REFERENCES mods(mod_id) ON DELETE CASCADE,
    fluid_id TEXT NOT NULL,
    PRIMARY KEY (mod_id, fluid_id)
);

CREATE TABLE translations (
    lang     TEXT NOT NULL,
    lang_key TEXT NOT NULL,
    name     TEXT NOT NULL,
    PRIMARY KEY (lang, lang_key)
);

CREATE INDEX ON translations (lang, lang_key);
CREATE INDEX ON translations (lang_key);

CREATE TABLE tags (
    id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE tag_members (
    tag_id      UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    item_mod_id TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    PRIMARY KEY (tag_id, item_mod_id, item_id),
    FOREIGN KEY (item_mod_id, item_id) REFERENCES items(mod_id, item_id) ON DELETE CASCADE
);

-- Stores raw tag file values per tag name for cross-JAR tag resolution.
CREATE TABLE tag_values (
    tag_name TEXT NOT NULL,
    value    TEXT NOT NULL,
    PRIMARY KEY (tag_name, value)
);

CREATE INDEX ON tag_values (tag_name);

CREATE TABLE machine_types (
    mod_id        TEXT  NOT NULL REFERENCES mods(mod_id) ON DELETE CASCADE,
    machine_id    TEXT  NOT NULL,
    name          TEXT  NOT NULL,
    -- Resolved i18n key for the display name (see LocalizeMachineNames); NULL falls back to name.
    name_lang_key TEXT,
    -- Which plugin evaluates this machine; NULL means the machine's own mod_id.
    ecosystem     TEXT,
    -- Everything mod-specific; the host never reads an individual field here.
    mod_data      JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (mod_id, machine_id)
);

-- Defines the input/output slot grid for each machine type.
CREATE TABLE machine_slots (
    mod_id     TEXT     NOT NULL,
    machine_id TEXT     NOT NULL,
    slot_index SMALLINT NOT NULL,
    slot_type  TEXT     NOT NULL
        CHECK (slot_type IN ('item_input', 'item_output', 'fluid_input', 'fluid_output')),
    slot_x     SMALLINT,
    slot_y     SMALLINT,
    label      TEXT,
    PRIMARY KEY (mod_id, machine_id, slot_index),
    FOREIGN KEY (mod_id, machine_id) REFERENCES machine_types(mod_id, machine_id) ON DELETE CASCADE
);

CREATE INDEX ON machine_slots (mod_id, machine_id);

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

-- shape: 9-element row-major 3×3 array for crafting_shaped recipes. NULL for non-shaped.
CREATE TABLE recipes (
    id             UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_mod_id TEXT   NOT NULL,
    machine_id     TEXT   NOT NULL,
    -- The mod whose modfile defines this recipe; may differ from machine_mod_id.
    source_mod_id  TEXT   NOT NULL REFERENCES mods(mod_id) ON DELETE CASCADE,
    name           TEXT,
    duration_ticks INT    NOT NULL,
    mod_data       JSONB  NOT NULL DEFAULT '{}'::jsonb,
    content_hash   TEXT,
    shape          TEXT[],
    FOREIGN KEY (machine_mod_id, machine_id) REFERENCES machine_types(mod_id, machine_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX recipes_content_hash_idx ON recipes (content_hash) WHERE content_hash IS NOT NULL;
CREATE INDEX ON recipes (machine_mod_id, machine_id);
CREATE INDEX ON recipes (source_mod_id);

CREATE TABLE recipe_item_inputs (
    id              UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID    NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    sort_index      SMALLINT NOT NULL DEFAULT 0,
    item_mod_id     TEXT,
    item_id         TEXT,
    tag_id          UUID    REFERENCES tags(id),
    amount_num      INT     NOT NULL,
    amount_den      INT     NOT NULL,
    probability_num INT     NOT NULL DEFAULT 1,
    probability_den INT     NOT NULL DEFAULT 1,
    non_consuming   BOOLEAN NOT NULL DEFAULT FALSE, -- reusable tool; not factored into consumption rates
    CONSTRAINT chk_item_or_tag CHECK (
        (item_mod_id IS NOT NULL AND item_id IS NOT NULL AND tag_id IS NULL) OR
        (item_mod_id IS NULL     AND item_id IS NULL     AND tag_id IS NOT NULL)
    )
);

CREATE TABLE recipe_item_outputs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    sort_index      SMALLINT NOT NULL DEFAULT 0,
    item_mod_id     TEXT NOT NULL,
    item_id         TEXT NOT NULL,
    amount_num      INT  NOT NULL,
    amount_den      INT  NOT NULL,
    probability_num INT  NOT NULL DEFAULT 1,
    probability_den INT  NOT NULL DEFAULT 1,
    FOREIGN KEY (item_mod_id, item_id) REFERENCES items(mod_id, item_id) ON DELETE CASCADE
);

CREATE TABLE recipe_fluid_inputs (
    id              UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID   NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    sort_index      SMALLINT NOT NULL DEFAULT 0,
    fluid_mod_id    TEXT,
    fluid_id        TEXT,
    tag_id          UUID   REFERENCES tags(id),
    amount_mb       BIGINT NOT NULL,
    probability_num INT    NOT NULL DEFAULT 1,
    probability_den INT    NOT NULL DEFAULT 1,
    CONSTRAINT recipe_fluid_inputs_ref_check CHECK (
        (fluid_mod_id IS NOT NULL AND fluid_id IS NOT NULL AND tag_id IS NULL) OR
        (fluid_mod_id IS NULL     AND fluid_id IS NULL     AND tag_id IS NOT NULL)
    ),
    FOREIGN KEY (fluid_mod_id, fluid_id) REFERENCES fluids(mod_id, fluid_id) ON DELETE CASCADE
);

CREATE TABLE recipe_fluid_outputs (
    id              UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID   NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    sort_index      SMALLINT NOT NULL DEFAULT 0,
    fluid_mod_id    TEXT,
    fluid_id        TEXT,
    tag_id          UUID   REFERENCES tags(id),
    amount_mb       BIGINT NOT NULL,
    probability_num INT    NOT NULL DEFAULT 1,
    probability_den INT    NOT NULL DEFAULT 1,
    CONSTRAINT recipe_fluid_outputs_ref_check CHECK (
        (fluid_mod_id IS NOT NULL AND fluid_id IS NOT NULL AND tag_id IS NULL) OR
        (fluid_mod_id IS NULL     AND fluid_id IS NULL     AND tag_id IS NOT NULL)
    ),
    FOREIGN KEY (fluid_mod_id, fluid_id) REFERENCES fluids(mod_id, fluid_id) ON DELETE CASCADE
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

CREATE INDEX ON valid_recipe_types (target_mod_id, target_machine_id);

-- Block loot drops: what a block yields when broken.
CREATE TABLE block_drops (
    id            UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    block_mod_id  TEXT    NOT NULL,
    block_item_id TEXT    NOT NULL,
    drop_mod_id   TEXT    NOT NULL,
    drop_item_id  TEXT    NOT NULL,
    min_count     INT     NOT NULL DEFAULT 1,
    max_count     INT     NOT NULL DEFAULT 1,
    condition     TEXT    NOT NULL DEFAULT 'normal'
        CHECK (condition IN ('normal', 'silk_touch', 'fortune')),
    FOREIGN KEY (block_mod_id, block_item_id) REFERENCES items(mod_id, item_id) ON DELETE CASCADE,
    FOREIGN KEY (drop_mod_id,  drop_item_id)  REFERENCES items(mod_id, item_id) ON DELETE CASCADE
);

CREATE INDEX ON block_drops (block_mod_id, block_item_id);
CREATE INDEX ON block_drops (drop_mod_id,  drop_item_id);

-- Villager trades: what each profession buys/sells at each tier.
--
-- source_mod_id names the mod the offer belongs to, not the mod of the traded
-- items: vanilla's optional trade_rebalance datapack redefines the very same
-- professions and tiers minecraft already defines, so without it the two sets
-- overwrite each other instead of coexisting.
--
-- trade_key is the offer's stable identity within its mod (the source file
-- stem for data-driven trades). Profession + tier + item pair does NOT
-- identify an offer: the cartographer alone sells nine distinct explorer maps
-- that all read as "emerald + compass -> map" and differ only in an NBT
-- modifier, and keying on the item pair silently collapses them to two rows.
CREATE TABLE villager_trades (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_mod_id    TEXT NOT NULL,
    trade_key        TEXT NOT NULL,
    profession       TEXT NOT NULL,
    tier             INT  NOT NULL CHECK (tier BETWEEN 1 AND 5),
    cost_mod_id      TEXT NOT NULL,
    cost_item_id     TEXT NOT NULL,
    cost_count       INT  NOT NULL DEFAULT 1,
    -- Optional second cost slot: vanilla charges two different items for a
    -- single offer 18 times (emerald + compass, emerald + book, ...).
    cost2_mod_id     TEXT,
    cost2_item_id    TEXT,
    cost2_count      INT,
    result_mod_id    TEXT NOT NULL,
    result_item_id   TEXT NOT NULL,
    result_count     INT  NOT NULL DEFAULT 1,
    result_modified  BOOL NOT NULL DEFAULT FALSE,
    -- The offer's price is not fixed in the data: an enchanted book costs what
    -- the enchantment decides. The counts above are then a floor, and saying so
    -- is the difference between a catalog that is right and one that looks it.
    cost_variable    BOOL NOT NULL DEFAULT FALSE,
    max_uses         INT,
    xp               INT,
    CHECK ((cost2_mod_id IS NULL) = (cost2_item_id IS NULL)),
    CHECK ((cost2_mod_id IS NULL) = (cost2_count  IS NULL)),
    FOREIGN KEY (source_mod_id)                   REFERENCES mods(mod_id) ON DELETE CASCADE,
    FOREIGN KEY (cost_mod_id,   cost_item_id)     REFERENCES items(mod_id, item_id) ON DELETE CASCADE,
    FOREIGN KEY (cost2_mod_id,  cost2_item_id)    REFERENCES items(mod_id, item_id) ON DELETE CASCADE,
    FOREIGN KEY (result_mod_id, result_item_id)   REFERENCES items(mod_id, item_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX villager_trades_unique_idx
    ON villager_trades (source_mod_id, profession, tier, trade_key);

CREATE INDEX ON villager_trades (source_mod_id);
CREATE INDEX ON villager_trades (profession, tier);
CREATE INDEX ON villager_trades (cost_mod_id,   cost_item_id);
CREATE INDEX ON villager_trades (cost2_mod_id,  cost2_item_id);
CREATE INDEX ON villager_trades (result_mod_id, result_item_id);
