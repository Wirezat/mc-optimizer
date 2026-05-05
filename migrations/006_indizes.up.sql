-- 006_indizes.up.sql
-- Performance-Indizes für häufige Queries

CREATE INDEX ON production_lines (factory_id, status);
CREATE INDEX ON machine_groups (pl_id, status);
CREATE INDEX ON pl_io (pl_id, direction);
CREATE INDEX ON recipes (machine_mod_id, machine_id);
CREATE INDEX ON tokens (user_id, type, expires_at);
CREATE INDEX ON solver_drafts (factory_id, user_id);
CREATE INDEX ON solver_drafts (expires_at);
