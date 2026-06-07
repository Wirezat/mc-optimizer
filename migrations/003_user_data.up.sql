-- 003_user_data.up.sql
-- User-owned data. Every table traces ownership back to users(id) via saves or directly.

CREATE TABLE saves (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE factories (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    save_id UUID NOT NULL REFERENCES saves(id) ON DELETE CASCADE,
    name    TEXT NOT NULL
);

CREATE TABLE production_lines (
    id             UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    factory_id     UUID    REFERENCES factories(id) ON DELETE CASCADE,
    parent_pl_id   UUID    REFERENCES production_lines(id) ON DELETE SET NULL,
    name           TEXT    NOT NULL,
    target_mod_id  TEXT    NOT NULL,
    target_item_id TEXT    NOT NULL,
    rate_num       INT     NOT NULL,
    rate_den       INT     NOT NULL,
    time_unit      TEXT    NOT NULL CHECK (time_unit IN ('t', 's', 'min', 'h')),
    optimize_mode  TEXT    NOT NULL CHECK (optimize_mode IN ('TARGET', 'AUTO')),
    status         TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived'))
);

CREATE TABLE pl_io (
    id            UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    pl_id         UUID   NOT NULL REFERENCES production_lines(id) ON DELETE CASCADE,
    direction     TEXT   NOT NULL CHECK (direction IN ('input', 'output')),
    io_type       TEXT   NOT NULL CHECK (io_type IN ('item', 'fluid')),
    mod_id        TEXT   NOT NULL,
    item_fluid_id TEXT   NOT NULL,
    rate_num      INT    NOT NULL,
    rate_den      INT    NOT NULL,
    is_stop_point BOOL   NOT NULL DEFAULT false
);

CREATE TABLE machine_groups (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pl_id           UUID NOT NULL REFERENCES production_lines(id) ON DELETE CASCADE,
    machine_mod_id  TEXT NOT NULL,
    machine_id      TEXT NOT NULL,
    recipe_id       UUID NOT NULL REFERENCES recipes(id),
    count           INT  NOT NULL,
    upgrade_tier_id UUID REFERENCES upgrade_tiers(id),
    upgrade_count   INT  NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'planned', 'built', 'archived')),
    FOREIGN KEY (machine_mod_id, machine_id) REFERENCES machine_types(mod_id, machine_id)
);

CREATE TABLE solver_drafts (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    factory_id UUID        NOT NULL REFERENCES factories(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    result     JSONB       NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '24 hours',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
