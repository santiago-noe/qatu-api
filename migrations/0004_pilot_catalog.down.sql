-- Revierte 0004_pilot_catalog: borra solo los datos que sembró.
DELETE FROM platform_settings WHERE city_id IS NULL AND category_id IS NULL AND key IN ('rental.owner_commission_bps', 'rental.client_service_fee_bps', 'service.provider_commission_bps', 'service.client_service_fee_bps');
DELETE FROM categories WHERE vertical = 'rental' AND parent_id IN (SELECT id FROM categories WHERE vertical = 'rental' AND slug IN ('construccion', 'carpinteria-y-taller', 'jardin', 'limpieza', 'pintura', 'eventos-y-audiovisual'));
DELETE FROM categories WHERE vertical = 'rental' AND parent_id IS NULL AND slug IN ('construccion', 'carpinteria-y-taller', 'jardin', 'limpieza', 'pintura', 'eventos-y-audiovisual');
DELETE FROM categories WHERE vertical = 'service' AND slug IN ('gasfiteria', 'electricidad', 'pintura', 'jardineria', 'carpinteria', 'cerrajeria', 'albanileria-menor', 'limpieza', 'instalacion-de-electrodomesticos', 'armado-de-muebles');
UPDATE users SET zone_id = NULL, city_id = NULL WHERE city_id IN (SELECT id FROM cities WHERE slug = 'ayacucho');
DELETE FROM zones WHERE city_id IN (SELECT id FROM cities WHERE slug = 'ayacucho');
DELETE FROM cities WHERE slug = 'ayacucho';
