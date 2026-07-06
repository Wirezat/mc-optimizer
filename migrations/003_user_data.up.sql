-- 003_user_data.up.sql
-- User-owned data. Every table traces ownership back to users(id) via saves or directly.

CREATE TABLE saves (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE factories (
    id      UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    save_id UUID    NOT NULL REFERENCES saves(id) ON DELETE CASCADE,
    name    TEXT    NOT NULL,
    src     BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE pl_groups (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    factory_id UUID NOT NULL REFERENCES factories(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    position   TEXT NOT NULL DEFAULT 'V',
    UNIQUE (factory_id, name)
);

CREATE TABLE production_lines (
    id             UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    factory_id     UUID    REFERENCES factories(id) ON DELETE CASCADE,
    pl_group_id    UUID    REFERENCES pl_groups(id) ON DELETE SET NULL,
    parent_pl_id   UUID    REFERENCES production_lines(id) ON DELETE SET NULL,
    name           TEXT    NOT NULL,
    target_mod_id  TEXT    NOT NULL,
    target_item_id TEXT    NOT NULL,
    rate_num       INT     NOT NULL,
    rate_den       INT     NOT NULL,
    time_unit      TEXT    NOT NULL CHECK (time_unit IN ('t', 's', 'min', 'h')),
    optimize_mode  TEXT    NOT NULL CHECK (optimize_mode IN ('TARGET', 'AUTO')),
    status         TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
    position       TEXT    NOT NULL DEFAULT 'V',
    -- The original SolveRequest, persisted so the PL can be re-solved later (e.g.
    -- "more output": re-run the solver at a higher target rate, reusing the same
    -- recipe choices). Stop points etc. are implied by the stored request.
    solve_request  JSONB
);

CREATE INDEX ON production_lines (factory_id, status);

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

CREATE INDEX ON pl_io (pl_id, direction);

CREATE TABLE machine_groups (
    id              UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
    pl_id           UUID   NOT NULL REFERENCES production_lines(id) ON DELETE CASCADE,
    machine_mod_id  TEXT   NOT NULL,
    machine_id      TEXT   NOT NULL,
    recipe_id       UUID   NOT NULL REFERENCES recipes(id),
    count           INT    NOT NULL,
    upgrade_tier_id UUID   REFERENCES upgrade_tiers(id),
    upgrade_count   INT    NOT NULL DEFAULT 0,
    status          TEXT   NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'planned', 'built', 'archived')),
    -- Fractional exact machine count (rational num/den), mirroring
    -- solver.MachineGroupDraft.ExactCount — lets upgrade edits recompute the count
    -- from the true required rate (lossless) instead of from the rounded count.
    exact_count_num BIGINT NOT NULL DEFAULT 0,
    exact_count_den BIGINT NOT NULL DEFAULT 1,
    FOREIGN KEY (machine_mod_id, machine_id) REFERENCES machine_types(mod_id, machine_id)
);

CREATE INDEX ON machine_groups (pl_id, status);

CREATE TABLE solver_drafts (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    factory_id UUID        NOT NULL REFERENCES factories(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    result     JSONB       NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '24 hours',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON solver_drafts (factory_id, user_id);
CREATE INDEX ON solver_drafts (expires_at);

CREATE TABLE factory_source_outputs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    factory_id UUID NOT NULL REFERENCES factories(id) ON DELETE CASCADE,
    mod_id     TEXT NOT NULL,
    item_id    TEXT NOT NULL,
    rate_num   INT  NOT NULL DEFAULT 1,
    rate_den   INT  NOT NULL DEFAULT 1,
    time_unit  TEXT NOT NULL DEFAULT 'min' CHECK (time_unit IN ('t', 's', 'min', 'h')),
    UNIQUE (factory_id, mod_id, item_id)
);

CREATE TABLE factory_source_inputs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    factory_id UUID NOT NULL REFERENCES factories(id) ON DELETE CASCADE,
    mod_id     TEXT NOT NULL,
    item_id    TEXT NOT NULL,
    rate_num   INT  NOT NULL DEFAULT 1,
    rate_den   INT  NOT NULL DEFAULT 1,
    time_unit  TEXT NOT NULL DEFAULT 'min' CHECK (time_unit IN ('t', 's', 'min', 'h')),
    UNIQUE (factory_id, mod_id, item_id)
);

CREATE TABLE user_active_machines (
    user_id    UUID NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
    mod_id     TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    PRIMARY KEY (user_id, mod_id, machine_id),
    FOREIGN KEY (mod_id, machine_id) REFERENCES machine_types(mod_id, machine_id) ON DELETE CASCADE
);

CREATE TABLE save_active_mods (
    save_id UUID NOT NULL REFERENCES saves(id)    ON DELETE CASCADE,
    mod_id  TEXT NOT NULL REFERENCES mods(mod_id) ON DELETE CASCADE,
    PRIMARY KEY (save_id, mod_id)
);

CREATE TABLE save_unlocked_items (
    save_id UUID NOT NULL REFERENCES saves(id)                ON DELETE CASCADE,
    mod_id  TEXT NOT NULL,
    item_id TEXT NOT NULL,
    PRIMARY KEY (save_id, mod_id, item_id),
    FOREIGN KEY (mod_id, item_id) REFERENCES items(mod_id, item_id) ON DELETE CASCADE
);

CREATE TABLE save_unlocked_fluids (
    save_id  UUID NOT NULL REFERENCES saves(id)                 ON DELETE CASCADE,
    mod_id   TEXT NOT NULL,
    fluid_id TEXT NOT NULL,
    PRIMARY KEY (save_id, mod_id, fluid_id),
    FOREIGN KEY (mod_id, fluid_id) REFERENCES fluids(mod_id, fluid_id) ON DELETE CASCADE
);
