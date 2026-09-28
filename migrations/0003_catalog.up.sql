-- 0003_catalog: ciudades, zonas con polígono, categorías (herramientas y oficios) y
-- configuración de la plataforma (feature 002). Los datos del piloto llegan en 0004.

-- Ciudad: unidad de expansión (docs/01). Se activa con `enabled` (feature flag).
CREATE TABLE cities (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug       text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  name       text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
  region     text NOT NULL,
  -- Ubigeo INEI de la provincia (Huamanga: 0501).
  ubigeo     text UNIQUE CHECK (ubigeo ~ '^[0-9]{4}$'),
  timezone   text NOT NULL DEFAULT 'America/Lima',
  -- Punto de referencia para centrar el mapa (plaza principal).
  center     geometry(Point, 4326) NOT NULL,
  enabled    boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER cities_set_updated_at BEFORE UPDATE ON cities
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Zona: distrito en el piloto (decisión de clarify); el modelo admite barrios más adelante.
CREATE TABLE zones (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  city_id    uuid NOT NULL REFERENCES cities (id) ON DELETE RESTRICT,
  slug       text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  name       text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
  -- Ubigeo INEI del distrito (Ayacucho: 050101).
  ubigeo     text UNIQUE CHECK (ubigeo ~ '^[0-9]{6}$'),
  -- Límite oficial; sin polígono la zona se elige de la lista pero no se detecta por ubicación.
  boundary   geometry(MultiPolygon, 4326) CHECK (boundary IS NULL OR ST_IsValid(boundary)),
  sort_order smallint NOT NULL DEFAULT 0,
  enabled    boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT zones_slug_per_city UNIQUE (city_id, slug),
  -- Permite que otras tablas exijan que la zona pertenezca a su ciudad.
  CONSTRAINT zones_id_city UNIQUE (id, city_id)
);

CREATE INDEX zones_boundary_gist ON zones USING gist (boundary);

CREATE TRIGGER zones_set_updated_at BEFORE UPDATE ON zones
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- zone_at: zona habilitada que contiene el punto (longitud, latitud), o NULL. ST_Covers incluye
-- el borde, así un punto sobre el límite entre dos distritos no queda sin zona.
CREATE FUNCTION zone_at(lng double precision, lat double precision) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT z.id
  FROM zones z
  JOIN cities c ON c.id = z.city_id
  WHERE z.enabled AND c.enabled
    AND ST_Covers(z.boundary, ST_SetSRID(ST_MakePoint(lng, lat), 4326))
  ORDER BY z.sort_order
  LIMIT 1
$$;

-- La ciudad y la zona del usuario (reservadas en 0002); la zona debe ser de esa ciudad.
ALTER TABLE users
  ADD CONSTRAINT users_city_fk FOREIGN KEY (city_id) REFERENCES cities (id) ON DELETE SET NULL,
  ADD CONSTRAINT users_zone_in_city FOREIGN KEY (zone_id, city_id) REFERENCES zones (id, city_id),
  ADD CONSTRAINT users_zone_needs_city CHECK (zone_id IS NULL OR city_id IS NOT NULL);

-- Categoría: herramientas (vertical rental) y oficios (vertical service) en un solo árbol de
-- 2 niveles (decisión de clarify). product y space quedan para las fases 3 (docs/01).
CREATE TABLE categories (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  vertical          text NOT NULL CHECK (vertical IN ('rental', 'service', 'product', 'space')),
  parent_id         uuid REFERENCES categories (id) ON DELETE RESTRICT,
  slug              text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  name              text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
  description       text CHECK (char_length(description) <= 280),
  -- Nombre de un ícono de lucide-react; la app lo dibuja.
  icon              text,
  sort_order        smallint NOT NULL DEFAULT 0,
  -- JSON Schema de los atributos propios (potencia, voltaje…); la web arma el formulario con él.
  attributes_schema jsonb NOT NULL DEFAULT '{"type": "object", "properties": {}}'
                    CHECK (jsonb_typeof(attributes_schema) = 'object'),
  -- Riesgo (docs/05): define el nivel de verificación mínimo para alquilar.
  risk_level        text NOT NULL DEFAULT 'medium' CHECK (risk_level IN ('low', 'medium', 'high')),
  -- Prohibida: nadie puede publicar en ella (spec 002).
  prohibited        boolean NOT NULL DEFAULT false,
  enabled           boolean NOT NULL DEFAULT true,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  version           integer NOT NULL DEFAULT 1,
  -- El mismo nombre puede existir en dos verticales: Pintura (herramientas) y Pintura (oficio).
  CONSTRAINT categories_slug_per_vertical UNIQUE (vertical, slug),
  CONSTRAINT categories_not_own_parent CHECK (parent_id <> id)
);

CREATE INDEX categories_parent ON categories (parent_id);

CREATE TRIGGER categories_set_updated_at BEFORE UPDATE ON categories
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Árbol de 2 niveles: el padre es raíz y de la misma vertical, y quien tiene hijos no puede
-- colgar de otro. Se valida aquí para que ningún camino (API, SQL a mano) lo rompa.
CREATE FUNCTION categories_check_tree() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  parent categories%ROWTYPE;
BEGIN
  IF NEW.parent_id IS NULL THEN
    RETURN NEW;
  END IF;
  SELECT * INTO parent FROM categories WHERE id = NEW.parent_id;
  IF parent.parent_id IS NOT NULL THEN
    RAISE EXCEPTION 'categories: máximo 2 niveles' USING ERRCODE = 'check_violation';
  END IF;
  IF parent.vertical <> NEW.vertical THEN
    RAISE EXCEPTION 'categories: el padre debe ser de la misma vertical' USING ERRCODE = 'check_violation';
  END IF;
  IF EXISTS (SELECT 1 FROM categories WHERE parent_id = NEW.id) THEN
    RAISE EXCEPTION 'categories: una categoría con hijos no puede tener padre' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER categories_tree BEFORE INSERT OR UPDATE OF parent_id, vertical ON categories
  FOR EACH ROW EXECUTE FUNCTION categories_check_tree();

-- Activación por ciudad (feature flag, P2): sin fila, la categoría sigue su `enabled` global.
CREATE TABLE category_city_overrides (
  category_id uuid NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
  city_id     uuid NOT NULL REFERENCES cities (id) ON DELETE CASCADE,
  enabled     boolean NOT NULL,
  PRIMARY KEY (category_id, city_id)
);

-- Configuración de la plataforma: comisiones, tarifas, timeouts y políticas. Alcance opcional
-- por ciudad y categoría; gana el más específico. Cada cambio queda en audit_log (historial).
-- Una transacción copia los valores al crearse (price_snapshot): un cambio posterior no la afecta.
CREATE TABLE platform_settings (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  key         text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
  city_id     uuid REFERENCES cities (id) ON DELETE CASCADE,
  category_id uuid REFERENCES categories (id) ON DELETE CASCADE,
  value       jsonb NOT NULL,
  description text,
  updated_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  version     integer NOT NULL DEFAULT 1,
  CONSTRAINT platform_settings_scope UNIQUE NULLS NOT DISTINCT (key, city_id, category_id)
);

CREATE TRIGGER platform_settings_set_updated_at BEFORE UPDATE ON platform_settings
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
