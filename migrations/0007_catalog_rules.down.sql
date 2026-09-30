DROP TRIGGER tool_listings_category ON tool_listings;
DROP FUNCTION tool_listings_check_category();
ALTER TABLE zones DROP CONSTRAINT zones_boundary_not_empty;
