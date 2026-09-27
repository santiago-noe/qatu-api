-- Revierte 0002_accounts.
DROP TRIGGER IF EXISTS audit_log_no_update_delete ON audit_log;
DROP FUNCTION IF EXISTS audit_log_immutable();
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS consents;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS auth_identities;
DROP TABLE IF EXISTS users;
DROP FUNCTION IF EXISTS set_updated_at();
