-- Revierte 0001_init. Falla si alguna tabla depende de estas extensiones, a propósito.
DROP EXTENSION IF EXISTS citext;
DROP EXTENSION IF EXISTS unaccent;
DROP EXTENSION IF EXISTS btree_gist;
DROP EXTENSION IF EXISTS postgis;
