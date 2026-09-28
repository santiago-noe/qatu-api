-- Revierte 0003_catalog.
DROP TABLE IF EXISTS platform_settings;
DROP TABLE IF EXISTS category_city_overrides;
DROP TRIGGER IF EXISTS categories_tree ON categories;
DROP FUNCTION IF EXISTS categories_check_tree();
DROP TABLE IF EXISTS categories;
ALTER TABLE users
  DROP CONSTRAINT IF EXISTS users_zone_needs_city,
  DROP CONSTRAINT IF EXISTS users_zone_in_city,
  DROP CONSTRAINT IF EXISTS users_city_fk;
DROP FUNCTION IF EXISTS zone_at(double precision, double precision);
DROP TABLE IF EXISTS zones;
DROP TABLE IF EXISTS cities;
