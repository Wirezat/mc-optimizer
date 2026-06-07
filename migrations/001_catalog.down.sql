-- 001_catalog.down.sql
-- Drop in reverse FK order.

DROP TABLE IF EXISTS machine_interfaces;
DROP TABLE IF EXISTS valid_recipe_types;
DROP TABLE IF EXISTS recipe_fluid_outputs;
DROP TABLE IF EXISTS recipe_fluid_inputs;
DROP TABLE IF EXISTS recipe_item_outputs;
DROP TABLE IF EXISTS recipe_item_inputs;
DROP TABLE IF EXISTS recipes;
DROP TABLE IF EXISTS upgrade_tiers;
DROP TABLE IF EXISTS machine_types;
DROP TABLE IF EXISTS tag_members;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS fluids;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS mods;
