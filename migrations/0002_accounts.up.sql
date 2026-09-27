-- 0002_accounts: cuentas, identidades de acceso, roles, consentimientos y auditoría (feature 001).
-- La cuenta (users) está separada de cómo se entra (auth_identities): agregar OTP por celular
-- (feature 022) es un nuevo valor de provider, sin cambiar estas tablas.

-- Mantiene updated_at al día en cualquier tabla que lo use.
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;

CREATE TABLE users (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email              citext UNIQUE,
  email_verified_at  timestamptz,
  -- Reservado para la feature 022 (formato E.164, por ejemplo +51987654321).
  phone              text UNIQUE CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
  phone_verified_at  timestamptz,
  name               text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
  avatar_url         text,
  -- Las FK a ciudades y zonas se agregan con la feature 002.
  city_id            uuid,
  zone_id            uuid,
  -- Declaración de mayoría de edad (18+); se confirma con el DNI en la feature 021.
  adult_declared_at  timestamptz,
  status             text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
  suspended_reason   text,
  -- Inicio del periodo de gracia de eliminación (30 días); luego se anonimiza.
  deletion_requested_at timestamptz,
  verification_level smallint NOT NULL DEFAULT 0 CHECK (verification_level BETWEEN 0 AND 3),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  version            integer NOT NULL DEFAULT 1,
  -- Una cuenta tiene correo o celular: en el piloto siempre correo; en 022 puede ser solo celular.
  CONSTRAINT users_email_or_phone CHECK (email IS NOT NULL OR phone IS NOT NULL),
  CONSTRAINT users_suspended_reason CHECK (status <> 'suspended' OR suspended_reason IS NOT NULL)
);

CREATE TRIGGER users_set_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Cada forma de entrar a una cuenta.
CREATE TABLE auth_identities (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id          uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  provider         text NOT NULL CHECK (provider IN ('password', 'google', 'phone_otp')),
  -- password: correo normalizado · google: "sub" del token · phone_otp: celular E.164.
  provider_subject text NOT NULL,
  -- Solo para password (argon2id); los demás proveedores no guardan secretos.
  secret_hash      text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  last_used_at     timestamptz,
  CONSTRAINT auth_identities_subject_unique UNIQUE (provider, provider_subject),
  CONSTRAINT auth_identities_one_per_provider UNIQUE (user_id, provider),
  CONSTRAINT auth_identities_secret_only_password CHECK ((provider = 'password') = (secret_hash IS NOT NULL))
);

CREATE TABLE user_roles (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role       text NOT NULL CHECK (role IN ('client', 'lender', 'provider', 'support', 'moderator', 'admin')),
  granted_by uuid REFERENCES users (id) ON DELETE SET NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, role)
);

-- Consentimientos por finalidad y versión (Ley 29733). Se revocan, no se borran.
CREATE TABLE consents (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose    text NOT NULL CHECK (purpose IN ('terms', 'privacy', 'marketing')),
  version    text NOT NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  ip         inet,
  CONSTRAINT consents_revoked_after_granted CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);
CREATE INDEX consents_user_purpose_idx ON consents (user_id, purpose);

-- Registro de auditoría inmutable: solo se inserta.
CREATE TABLE audit_log (
  id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor_id  uuid,
  action    text NOT NULL,
  entity    text NOT NULL,
  entity_id text,
  before    jsonb,
  after     jsonb,
  ip        inet,
  at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id, at);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_id, at);

CREATE FUNCTION audit_log_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_log es inmutable: no se permite %', TG_OP;
END;
$$;

CREATE TRIGGER audit_log_no_update_delete BEFORE UPDATE OR DELETE ON audit_log
  FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();
