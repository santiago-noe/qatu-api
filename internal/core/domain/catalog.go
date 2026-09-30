package domain

import "encoding/json"

// Vertical separa el catálogo por tipo de oferta (docs/01). El piloto usa rental y service;
// product y space llegan en la fase 3 sin cambiar el modelo.
type Vertical string

const (
	VerticalRental  Vertical = "rental"
	VerticalService Vertical = "service"
	VerticalProduct Vertical = "product"
	VerticalSpace   Vertical = "space"
)

// ParseVertical valida una vertical recibida desde fuera (API).
func ParseVertical(raw string) (Vertical, bool) {
	switch v := Vertical(raw); v {
	case VerticalRental, VerticalService, VerticalProduct, VerticalSpace:
		return v, true
	}
	return "", false
}

// RiskLevel define el nivel de verificación exigido para alquilar (docs/05).
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// Category es un nodo del árbol de 2 niveles: raíz (Construcción) o tipo (Rotomartillo).
type Category struct {
	ID          string
	Vertical    Vertical
	ParentID    string // vacío en las raíces
	Slug        string
	Name        string
	Description string
	Icon        string // nombre de un ícono de lucide-react
	SortOrder   int
	// AttributesSchema es un JSON Schema; la web arma con él el formulario de la publicación.
	AttributesSchema json.RawMessage
	RiskLevel        RiskLevel
	Prohibited       bool
	Enabled          bool
	Children         []Category
}

// BuildCategoryTree arma el árbol desde una lista plana ordenada. Un tipo cuyo padre no está en la
// lista (apagado o prohibido) no se muestra: nadie debe ver Rotomartillo si Construcción está apagada.
func BuildCategoryTree(flat []Category) []Category {
	var roots []Category
	index := map[string]int{}
	for _, c := range flat {
		if c.ParentID == "" {
			index[c.ID] = len(roots)
			roots = append(roots, c)
		}
	}
	for _, c := range flat {
		if i, ok := index[c.ParentID]; ok && c.ParentID != "" {
			roots[i].Children = append(roots[i].Children, c)
		}
	}
	return roots
}

// GeoPoint es una coordenada WGS 84 (la de un GPS o del navegador).
type GeoPoint struct {
	Lat float64
	Lng float64
}

func (p GeoPoint) Validate() error {
	if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
		return ErrInvalidLocation
	}
	return nil
}

// City es la unidad de expansión (docs/01): se activa con Enabled.
type City struct {
	ID       string
	Slug     string
	Name     string
	Region   string
	Ubigeo   string // INEI de la provincia (Huamanga: 0501); opcional
	Timezone string
	Center   GeoPoint
	Enabled  bool
}

// Zone es un distrito en el piloto (decisión de clarify). HasBoundary indica si se puede
// detectar por ubicación; sin polígono solo se elige de la lista.
type Zone struct {
	ID          string
	CityID      string
	Slug        string
	Name        string
	Ubigeo      string
	SortOrder   int
	Enabled     bool
	HasBoundary bool
	// Boundary es el límite en GeoJSON (MultiPolygon). Solo lo leen y escriben el admin y la base;
	// al leer una lista llega simplificado, para dibujarlo.
	Boundary json.RawMessage
}

// CategoryCityScope es cómo está una categoría en una ciudad: Override nil = sigue su valor global.
type CategoryCityScope struct {
	City     City
	Override *bool
}

// ActiveFor dice si la categoría se ofrece en la ciudad: una prohibida nunca.
func (s CategoryCityScope) ActiveFor(c Category) bool {
	if c.Prohibited {
		return false
	}
	if s.Override != nil {
		return *s.Override
	}
	return c.Enabled
}

// Location es la ciudad y la zona de un punto (detección por ubicación).
type Location struct {
	City City
	Zone Zone
}

// EffectiveSchema es el esquema de atributos con el que se publica en un tipo: el de su categoría
// raíz (Construcción define marca, modelo, potencia…) más el propio del tipo, que agrega campos o
// redefine los de la raíz. "required" se une; si alguno cierra los campos extra
// (additionalProperties: false), el resultado también. Una raíz no hereda nada.
func (c Category) EffectiveSchema(parent Category) json.RawMessage {
	if c.ParentID == "" || c.ParentID != parent.ID {
		return c.AttributesSchema
	}
	var root, own map[string]any
	if json.Unmarshal(parent.AttributesSchema, &root) != nil || json.Unmarshal(c.AttributesSchema, &own) != nil || root == nil {
		return c.AttributesSchema // el validador rechazará un esquema roto
	}
	merged := make(map[string]any, len(root)+len(own))
	for k, v := range root {
		merged[k] = v
	}
	for k, v := range own {
		switch k {
		case "properties", "required", "additionalProperties":
		default:
			merged[k] = v
		}
	}
	props := map[string]any{}
	for _, schema := range []map[string]any{root, own} {
		if p, ok := schema["properties"].(map[string]any); ok {
			for name, def := range p {
				props[name] = def
			}
		}
	}
	merged["properties"] = props
	var required []any
	seen := map[any]bool{}
	for _, schema := range []map[string]any{root, own} {
		if r, ok := schema["required"].([]any); ok {
			for _, name := range r {
				if !seen[name] {
					seen[name] = true
					required = append(required, name)
				}
			}
		}
	}
	if len(required) > 0 {
		merged["required"] = required
	}
	if root["additionalProperties"] == false || own["additionalProperties"] == false {
		merged["additionalProperties"] = false
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return c.AttributesSchema
	}
	return out
}
