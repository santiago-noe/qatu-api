ALTER TABLE tool_listings DROP CONSTRAINT tool_listings_complete;
ALTER TABLE tool_listings ADD CONSTRAINT tool_listings_complete CHECK (
  status = 'draft' OR (
    zone_id IS NOT NULL AND price_day IS NOT NULL AND replacement_value IS NOT NULL
    AND deposit IS NOT NULL AND (pickup_enabled OR delivery_enabled)));
