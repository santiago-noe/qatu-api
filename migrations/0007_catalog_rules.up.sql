-- 0007_catalog_rules: reglas de la 002 que completan el alta de ciudades desde el admin y el
-- bloqueo de categorías prohibidas en las publicaciones de la 003.

-- Un límite que ST_MakeValid deja sin área (una línea, un punto) no sirve para detectar la zona.
ALTER TABLE zones ADD CONSTRAINT zones_boundary_not_empty
  CHECK (boundary IS NULL OR NOT ST_IsEmpty(boundary));

-- Una publicación va en un tipo de herramienta (segundo nivel de la vertical rental) y no puede
-- estar en revisión ni publicada en una categoría prohibida, ni la suya ni su raíz (spec 002).
-- El servicio lo revisa antes (CheckListingCategory); aquí se garantiza para cualquier camino.
CREATE FUNCTION tool_listings_check_category() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  cat    categories%ROWTYPE;
  parent categories%ROWTYPE;
BEGIN
  SELECT * INTO cat FROM categories WHERE id = NEW.category_id;
  IF cat.vertical <> 'rental' OR cat.parent_id IS NULL THEN
    RAISE EXCEPTION 'tool_listings: la categoría debe ser un tipo de herramienta' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.status IN ('in_review', 'published') THEN
    SELECT * INTO parent FROM categories WHERE id = cat.parent_id;
    IF cat.prohibited OR parent.prohibited THEN
      RAISE EXCEPTION 'tool_listings: categoría prohibida' USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER tool_listings_category BEFORE INSERT OR UPDATE OF status, category_id ON tool_listings
  FOR EACH ROW EXECUTE FUNCTION tool_listings_check_category();
