DELETE FROM platform_settings WHERE key LIKE 'listings.%';

DROP TABLE availability_blocks;
DROP TABLE listing_photos;
DROP TABLE listing_delivery_zones;
DROP TABLE tool_listings;
DROP TABLE lender_profiles;

DELETE FROM consents WHERE purpose = 'lender_terms';
ALTER TABLE consents DROP CONSTRAINT consents_purpose_check;
ALTER TABLE consents ADD CONSTRAINT consents_purpose_check
  CHECK (purpose IN ('terms', 'privacy', 'marketing'));
