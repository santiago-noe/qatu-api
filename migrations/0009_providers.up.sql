-- 0009_providers: perfiles de proveedores de servicios (feature 004): oficios con tarifa por hora
-- y paquetes, distritos de cobertura, horario semanal y días bloqueados. Dinero en céntimos (PEN).

-- Condiciones de proveedor: un consentimiento más, versionado y revocable (Ley 29733).
ALTER TABLE consents DROP CONSTRAINT consents_purpose_check;
ALTER TABLE consents ADD CONSTRAINT consents_purpose_check
  CHECK (purpose IN ('terms', 'privacy', 'marketing', 'lender_terms', 'provider_terms'));

-- Un perfil por persona (decisión de clarify: sin equipos en el MVP). Sigue el ciclo de
-- moderación de las publicaciones, sin archivar. La primera aprobación es la revisión manual del
-- nivel P (docs/05) y queda en verified_at: sin ella el perfil no se muestra.
CREATE TABLE provider_profiles (
  user_id            uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  business_name      text CHECK (char_length(btrim(business_name)) BETWEEN 2 AND 80),
  -- Celular peruano normalizado: privado hasta confirmar un trabajo (docs/05).
  phone              text NOT NULL CHECK (phone ~ '^\+519[0-9]{8}$'),
  city_id            uuid NOT NULL REFERENCES cities (id) ON DELETE RESTRICT,
  bio                text CHECK (char_length(bio) <= 2000),
  years_experience   smallint NOT NULL DEFAULT 0 CHECK (years_experience BETWEEN 0 AND 60),
  work_warranty_days smallint NOT NULL DEFAULT 15 CHECK (work_warranty_days BETWEEN 0 AND 90),
  accepts_urgent     boolean NOT NULL DEFAULT false,
  status             text NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'in_review', 'published', 'paused', 'rejected')),
  rejection_reason   text CHECK (char_length(rejection_reason) <= 500),
  verified_at        timestamptz,
  first_published_at timestamptz,
  version            integer NOT NULL DEFAULT 1,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT provider_profiles_rejected_reason CHECK (status <> 'rejected' OR rejection_reason IS NOT NULL),
  CONSTRAINT provider_profiles_live_verified CHECK (status NOT IN ('published', 'paused') OR verified_at IS NOT NULL)
);

-- Búsqueda (005) y cola de moderación: por estado y ciudad.
CREATE INDEX provider_profiles_status_city_idx ON provider_profiles (status, city_id);

CREATE TRIGGER provider_profiles_set_updated_at BEFORE UPDATE ON provider_profiles
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Oficios del proveedor. Sin tarifa por hora ni paquetes, el oficio es "a cotizar" (008).
CREATE TABLE provider_trades (
  provider_id uuid NOT NULL REFERENCES provider_profiles (user_id) ON DELETE CASCADE,
  category_id uuid NOT NULL REFERENCES categories (id) ON DELETE RESTRICT,
  hourly_rate bigint CHECK (hourly_rate > 0),
  min_hours   smallint NOT NULL DEFAULT 1 CHECK (min_hours BETWEEN 1 AND 8),
  sort_order  smallint NOT NULL DEFAULT 0,
  PRIMARY KEY (provider_id, category_id)
);

-- Búsqueda por oficio (005).
CREATE INDEX provider_trades_category_idx ON provider_trades (category_id);

-- Un oficio es una categoría raíz de la vertical service. El servicio lo revisa antes; aquí se
-- garantiza para cualquier camino.
CREATE FUNCTION provider_trades_check_category() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM categories WHERE id = NEW.category_id AND vertical = 'service' AND parent_id IS NULL) THEN
    RAISE EXCEPTION 'provider_trades: la categoría debe ser un oficio' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER provider_trades_category BEFORE INSERT OR UPDATE OF category_id ON provider_trades
  FOR EACH ROW EXECUTE FUNCTION provider_trades_check_category();

-- Paquetes a precio fijo de un oficio. Duración estimada en medias horas: ocupa esa franja (009).
CREATE TABLE service_packages (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id      uuid NOT NULL,
  category_id      uuid NOT NULL,
  title            text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 5 AND 80),
  description      text CHECK (char_length(description) <= 500),
  currency         text NOT NULL DEFAULT 'PEN' CHECK (currency = 'PEN'),
  price            bigint NOT NULL CHECK (price > 0),
  duration_minutes smallint NOT NULL CHECK (duration_minutes BETWEEN 30 AND 480 AND duration_minutes % 30 = 0),
  sort_order       smallint NOT NULL DEFAULT 0,
  created_at       timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (provider_id, category_id) REFERENCES provider_trades (provider_id, category_id) ON DELETE CASCADE
);

CREATE INDEX service_packages_trade_idx ON service_packages (provider_id, category_id, sort_order);

-- Distritos donde trabaja: se publica la cobertura, nunca su dirección (docs/03).
CREATE TABLE provider_coverage_zones (
  provider_id uuid NOT NULL REFERENCES provider_profiles (user_id) ON DELETE CASCADE,
  zone_id     uuid NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
  PRIMARY KEY (provider_id, zone_id)
);

-- Búsqueda por distrito (005).
CREATE INDEX provider_coverage_zones_zone_idx ON provider_coverage_zones (zone_id);

-- Horario semanal en la hora local de la ciudad: [start_minute, end_minute) en minutos desde las
-- 00:00, en medias horas. weekday ISO 8601 (1 = lunes). Las franjas de un día no se cruzan.
CREATE TABLE weekly_availability (
  provider_id  uuid NOT NULL REFERENCES provider_profiles (user_id) ON DELETE CASCADE,
  weekday      smallint NOT NULL CHECK (weekday BETWEEN 1 AND 7),
  start_minute smallint NOT NULL CHECK (start_minute BETWEEN 0 AND 1410 AND start_minute % 30 = 0),
  end_minute   smallint NOT NULL CHECK (end_minute BETWEEN 30 AND 1440 AND end_minute % 30 = 0),
  PRIMARY KEY (provider_id, weekday, start_minute),
  CONSTRAINT weekly_availability_order CHECK (start_minute < end_minute),
  CONSTRAINT weekly_availability_no_overlap
    EXCLUDE USING gist (provider_id WITH =, weekday WITH =, int4range(start_minute, end_minute) WITH &&)
);

-- Días bloqueados del proveedor y, con la 009, sus trabajos programados. Como en el calendario de
-- herramientas, dos bloqueos no se cruzan: sin doble reserva (constitución V).
CREATE TABLE provider_blocks (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_id uuid NOT NULL REFERENCES provider_profiles (user_id) ON DELETE CASCADE,
  period      tstzrange NOT NULL CHECK (NOT isempty(period) AND NOT lower_inf(period) AND NOT upper_inf(period)),
  reason      text NOT NULL CHECK (reason IN ('manual', 'job')),
  note        text CHECK (char_length(note) <= 200),
  created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT provider_blocks_no_overlap EXCLUDE USING gist (provider_id WITH =, period WITH &&)
);
