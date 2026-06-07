-- 004_indexes.down.sql

DROP INDEX IF EXISTS production_lines_factory_id_status_idx;
DROP INDEX IF EXISTS machine_groups_pl_id_status_idx;
DROP INDEX IF EXISTS pl_io_pl_id_direction_idx;
DROP INDEX IF EXISTS recipes_machine_mod_id_machine_id_idx;
DROP INDEX IF EXISTS tokens_user_id_type_expires_at_idx;
DROP INDEX IF EXISTS solver_drafts_factory_id_user_id_idx;
DROP INDEX IF EXISTS solver_drafts_expires_at_idx;
DROP INDEX IF EXISTS valid_recipe_types_target_idx;
