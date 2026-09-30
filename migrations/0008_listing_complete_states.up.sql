-- 0008_listing_complete_states: los datos completos se exigen solo donde la publicación está a la
-- vista o en revisión (in_review, published, paused). Un borrador incompleto se puede archivar y
-- una rechazada se corrige de a pocos antes de volver a enviarla (en 0006 ambas fallaban).
ALTER TABLE tool_listings DROP CONSTRAINT tool_listings_complete;
ALTER TABLE tool_listings ADD CONSTRAINT tool_listings_complete CHECK (
  status NOT IN ('in_review', 'published', 'paused') OR (
    zone_id IS NOT NULL AND price_day IS NOT NULL AND replacement_value IS NOT NULL
    AND deposit IS NOT NULL AND (pickup_enabled OR delivery_enabled)));
