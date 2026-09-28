-- Revierte 0005_category_risk_levels: vuelve al riesgo medio de los datos del piloto.
UPDATE categories SET risk_level = 'medium'
WHERE vertical = 'rental' AND parent_id IS NOT NULL
  AND slug IN ('motosierra', 'andamios', 'desbrozadora', 'cepilladora',
               'escaleras', 'lustradora', 'aspiradora-industrial', 'proyector');
