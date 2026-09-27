-- 0001_init: extensiones base que exige la arquitectura (docs/04).
-- Las tablas de negocio llegan con sus features (spec primero, constitución I).

-- Ubicaciones, distancias y ubicación pública ofuscada (mapa).
CREATE EXTENSION IF NOT EXISTS postgis;
-- Restricción de exclusión sobre rangos de fechas: sin doble reserva (constitución V).
CREATE EXTENSION IF NOT EXISTS btree_gist;
-- Búsqueda que tolera tildes (feature 005).
CREATE EXTENSION IF NOT EXISTS unaccent;
-- Correos sin distinguir mayúsculas.
CREATE EXTENSION IF NOT EXISTS citext;
