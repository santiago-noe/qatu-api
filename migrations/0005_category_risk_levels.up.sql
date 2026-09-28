-- 0005_category_risk_levels: riesgo por tipo de herramienta (decisión de negocio 2026-09-28).
-- El riesgo define el nivel de verificación exigido para alquilar (docs/05):
--   alto: cortes o trabajo en altura · bajo: uso doméstico sin peligro relevante · medio: el resto.
UPDATE categories SET risk_level = 'high'
WHERE vertical = 'rental' AND parent_id IS NOT NULL
  AND slug IN ('motosierra', 'andamios', 'desbrozadora', 'cepilladora');

UPDATE categories SET risk_level = 'low'
WHERE vertical = 'rental' AND parent_id IS NOT NULL
  AND slug IN ('escaleras', 'lustradora', 'aspiradora-industrial', 'proyector');
