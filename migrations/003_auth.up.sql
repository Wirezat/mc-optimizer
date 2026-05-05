-- 003_auth.up.sql
-- Nutzer, Tokens, und FK-Ergänzung auf mods

CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT        NOT NULL UNIQUE,
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL,
    type        TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);

-- FK auf mods.created_by nachträglich ergänzen (war in 001 nullable ohne FK)
ALTER TABLE mods
    ADD CONSTRAINT fk_mods_created_by
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;
