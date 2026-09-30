-- 0006_listings: perfil de arrendador, publicaciones de herramientas, fotos, delivery y
-- calendario (feature 003). Dinero en céntimos enteros, PEN (constitución IV).

-- Condiciones de arrendador: un consentimiento más, versionado y revocable (Ley 29733).
ALTER TABLE consents DROP CONSTRAINT consents_purpose_check;
ALTER TABLE consents ADD CONSTRAINT consents_purpose_check
  CHECK (purpose IN ('terms', 'privacy', 'marketing', 'lender_terms'));

-- Perfil de arrendador: activarlo da el rol `lender` (decisión de clarify). El celular es
-- privado: solo se revela a la contraparte tras confirmar una reserva (docs/05).
CREATE TABLE lender_profiles (
  user_id       uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  kind          text NOT NULL DEFAULT 'person' CHECK (kind IN ('person', 'business')),
  business_name text CHECK (char_length(btrim(business_name)) BETWEEN 2 AND 80),
  -- Celular peruano normalizado: +519XXXXXXXX.
  phone         text NOT NULL CHECK (phone ~ '^\+519[0-9]{8}$'),
  city_id       uuid NOT NULL REFERENCES cities (id) ON DELETE RESTRICT,
  zone_id       uuid NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT lender_profiles_zone_in_city FOREIGN KEY (zone_id, city_id) REFERENCES zones (id, city_id),
  CONSTRAINT lender_profiles_business_name CHECK (kind = 'person' OR business_name IS NOT NULL)
);

CREATE TRIGGER lender_profiles_set_updated_at BEFORE UPDATE ON lender_profiles
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Publicación de una herramienta: 1 publicación = 1 unidad (decisión de clarify).
-- En `draft` los datos pueden estar incompletos (el asistente guarda paso a paso); en cualquier
-- otro estado la base exige lo mínimo para alquilar sin ambigüedad.
CREATE TABLE tool_listings (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id             uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  category_id          uuid NOT NULL REFERENCES categories (id) ON DELETE RESTRICT,
  city_id              uuid NOT NULL REFERENCES cities (id) ON DELETE RESTRICT,
  zone_id              uuid,
  title                text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 5 AND 80),
  description          text CHECK (char_length(description) <= 2000),
  -- Atributos de la categoría (marca, modelo, potencia…), validados con su JSON Schema.
  attributes           jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(attributes) = 'object'),
  currency             text NOT NULL DEFAULT 'PEN' CHECK (currency = 'PEN'),
  replacement_value    bigint CHECK (replacement_value > 0),
  deposit              bigint CHECK (deposit >= 0),
  price_hour           bigint CHECK (price_hour > 0),
  price_day            bigint CHECK (price_day > 0),
  price_weekend        bigint CHECK (price_weekend > 0),
  price_week           bigint CHECK (price_week > 0),
  price_month          bigint CHECK (price_month > 0),
  -- Checklist de accesorios incluidos: se usa en la entrega y la devolución (007).
  accessories          text[] NOT NULL DEFAULT '{}' CHECK (cardinality(accessories) <= 30),
  usage_instructions   text CHECK (char_length(usage_instructions) <= 2000),
  pickup_enabled       boolean NOT NULL DEFAULT false,
  -- Punto exacto: privado, nunca en la ficha pública ni en la caché (docs/03).
  pickup_location      geometry(Point, 4326),
  -- Punto desplazado de forma determinista dentro de public_radius_m: el que ve el público.
  public_location      geometry(Point, 4326),
  public_radius_m      integer CHECK (public_radius_m BETWEEN 100 AND 3000),
  delivery_enabled     boolean NOT NULL DEFAULT false,
  delivery_fee         bigint CHECK (delivery_fee >= 0),
  booking_mode         text NOT NULL DEFAULT 'request' CHECK (booking_mode IN ('request', 'instant')),
  cancel_policy        text NOT NULL DEFAULT 'moderate' CHECK (cancel_policy IN ('flexible', 'moderate', 'strict')),
  -- Nivel mínimo del arrendatario (docs/05); nunca menor al que pide el riesgo de la categoría.
  min_verification     smallint NOT NULL DEFAULT 1 CHECK (min_verification BETWEEN 0 AND 2),
  min_notice_hours     smallint NOT NULL DEFAULT 12 CHECK (min_notice_hours BETWEEN 0 AND 168),
  min_duration_hours   smallint NOT NULL DEFAULT 24 CHECK (min_duration_hours BETWEEN 1 AND 720),
  max_duration_hours   smallint NOT NULL DEFAULT 720 CHECK (max_duration_hours BETWEEN 1 AND 2160),
  status               text NOT NULL DEFAULT 'draft'
                         CHECK (status IN ('draft', 'in_review', 'published', 'paused', 'rejected', 'archived')),
  rejection_reason     text CHECK (char_length(rejection_reason) <= 500),
  first_published_at   timestamptz,
  version              integer NOT NULL DEFAULT 1,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT tool_listings_zone_in_city FOREIGN KEY (zone_id, city_id) REFERENCES zones (id, city_id),
  CONSTRAINT tool_listings_duration CHECK (min_duration_hours <= max_duration_hours),
  CONSTRAINT tool_listings_rejected_reason CHECK (status <> 'rejected' OR rejection_reason IS NOT NULL),
  CONSTRAINT tool_listings_pickup CHECK (
    NOT pickup_enabled OR (pickup_location IS NOT NULL AND public_location IS NOT NULL AND public_radius_m IS NOT NULL)),
  CONSTRAINT tool_listings_delivery CHECK (NOT delivery_enabled OR delivery_fee IS NOT NULL),
  CONSTRAINT tool_listings_complete CHECK (
    status = 'draft' OR (
      zone_id IS NOT NULL AND price_day IS NOT NULL AND replacement_value IS NOT NULL
      AND deposit IS NOT NULL AND (pickup_enabled OR delivery_enabled)))
);

CREATE INDEX tool_listings_owner_idx ON tool_listings (owner_id, status);
-- Búsqueda (005) y cola de moderación: por estado, ciudad y categoría.
CREATE INDEX tool_listings_status_city_idx ON tool_listings (status, city_id, category_id);
CREATE INDEX tool_listings_public_location_gist ON tool_listings USING gist (public_location);

CREATE TRIGGER tool_listings_set_updated_at BEFORE UPDATE ON tool_listings
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Distritos que cubre el delivery de una publicación.
CREATE TABLE listing_delivery_zones (
  listing_id uuid NOT NULL REFERENCES tool_listings (id) ON DELETE CASCADE,
  zone_id    uuid NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
  PRIMARY KEY (listing_id, zone_id)
);

-- Fotos: públicas (mínimo 3, máximo 12) y una privada de la placa o número de serie, que solo
-- ven el dueño y soporte (disputas). El archivo vive en S3; aquí su clave y su estado.
CREATE TABLE listing_photos (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  listing_id  uuid NOT NULL REFERENCES tool_listings (id) ON DELETE CASCADE,
  kind        text NOT NULL CHECK (kind IN ('public', 'serial')),
  object_key  text NOT NULL UNIQUE,
  -- pending: subida pedida; ready: procesada (tamaños generados, sin EXIF); failed: no es una imagen válida.
  status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
  width       integer CHECK (width > 0),
  height      integer CHECK (height > 0),
  sort_order  smallint NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX listing_photos_listing_idx ON listing_photos (listing_id, kind, sort_order);
CREATE UNIQUE INDEX listing_photos_one_serial ON listing_photos (listing_id) WHERE kind = 'serial';

-- Calendario: bloqueos manuales, reservas y holds (006). La restricción de exclusión impide que
-- dos bloqueos de la misma publicación se crucen: sin doble reserva (constitución V).
CREATE TABLE availability_blocks (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  listing_id  uuid NOT NULL REFERENCES tool_listings (id) ON DELETE CASCADE,
  period      tstzrange NOT NULL CHECK (NOT isempty(period) AND NOT lower_inf(period) AND NOT upper_inf(period)),
  reason      text NOT NULL CHECK (reason IN ('manual', 'booking', 'hold')),
  note        text CHECK (char_length(note) <= 200),
  created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT availability_blocks_no_overlap EXCLUDE USING gist (listing_id WITH =, period WITH &&)
);

-- Ajustes de la feature (decisión de clarify), editables con historial desde el admin.
INSERT INTO platform_settings (key, value, description) VALUES
  ('listings.deposit_low_bps', '2000', 'Garantía sugerida: % del valor de reposición en categorías de riesgo bajo.'),
  ('listings.deposit_medium_bps', '3000', 'Garantía sugerida: % del valor de reposición en categorías de riesgo medio.'),
  ('listings.deposit_high_bps', '5000', 'Garantía sugerida: % del valor de reposición en categorías de riesgo alto.'),
  ('listings.deposit_min_factor_bps', '5000', 'La garantía puede bajar hasta este % de la sugerida.'),
  ('listings.deposit_max_factor_bps', '15000', 'La garantía puede subir hasta este % de la sugerida.'),
  ('listings.public_radius_m', '500', 'Radio en metros del círculo público en el mapa: nunca se muestra el punto exacto.');
