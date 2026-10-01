DROP TABLE provider_blocks;
DROP TABLE weekly_availability;
DROP TABLE provider_coverage_zones;
DROP TABLE service_packages;
DROP TABLE provider_trades;
DROP FUNCTION provider_trades_check_category();
DROP TABLE provider_profiles;

DELETE FROM consents WHERE purpose = 'provider_terms';
ALTER TABLE consents DROP CONSTRAINT consents_purpose_check;
ALTER TABLE consents ADD CONSTRAINT consents_purpose_check
  CHECK (purpose IN ('terms', 'privacy', 'marketing', 'lender_terms'));
