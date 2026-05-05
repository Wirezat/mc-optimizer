-- 003_auth.down.sql
ALTER TABLE mods DROP CONSTRAINT IF EXISTS fk_mods_created_by;
DROP TABLE IF EXISTS tokens;
DROP TABLE IF EXISTS users;
