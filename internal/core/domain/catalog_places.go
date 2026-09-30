package domain

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Alta de ciudades y distritos desde el admin (spec 002). Los límites llegan en GeoJSON, como los
// exportan OpenStreetMap (admin_level 8) o el INEI.
const (
	// BoundaryMaxVertices acota el tamaño de un límite: un distrito de OSM tiene cientos a pocos miles.
	BoundaryMaxVertices = 20_000
	// ZoneMaxDistanceM: el centro del límite no puede quedar a más de esto del centro de la ciudad.
	// Atrapa el error más común al exportar: latitud y longitud invertidas.
	ZoneMaxDistanceM = 60_000
	// MaxZoneOverlap: fracción del área que un distrito puede compartir con otro de la misma ciudad.
	// Dos límites oficiales vecinos solo se tocan en el borde; más que esto es un distrito repetido.
	MaxZoneOverlap = 0.01
)

var (
	cityUbigeoPattern = regexp.MustCompile(`^[0-9]{4}$`)
	zoneUbigeoPattern = regexp.MustCompile(`^[0-9]{6}$`)
)

// placeName limpia espacios y valida el largo (el mismo CHECK de la migración 0003).
func placeName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")
	return name, name != "" && utf8.RuneCountInString(name) <= CategoryNameMax
}

// NormalizeCity valida una ciudad nueva o editada. La zona horaria la pone la base (America/Lima).
func NormalizeCity(c City) (City, error) {
	c.Slug = strings.TrimSpace(c.Slug)
	c.Ubigeo = strings.TrimSpace(c.Ubigeo)
	var okName, okRegion bool
	c.Name, okName = placeName(c.Name)
	c.Region, okRegion = placeName(c.Region)
	switch {
	case !slugPattern.MatchString(c.Slug):
		return City{}, ErrInvalidSlug
	case !okName || !okRegion:
		return City{}, ErrInvalidName
	case c.Ubigeo != "" && !cityUbigeoPattern.MatchString(c.Ubigeo):
		return City{}, ErrInvalidUbigeo
	}
	if err := c.Center.Validate(); err != nil {
		return City{}, err
	}
	return c, nil
}

// NormalizeZone valida un distrito de la ciudad. Su ubigeo empieza con el de la provincia.
func NormalizeZone(z Zone, city City) (Zone, error) {
	z.Slug = strings.TrimSpace(z.Slug)
	z.Ubigeo = strings.TrimSpace(z.Ubigeo)
	var ok bool
	z.Name, ok = placeName(z.Name)
	switch {
	case !slugPattern.MatchString(z.Slug):
		return Zone{}, ErrInvalidSlug
	case !ok:
		return Zone{}, ErrInvalidName
	case z.Ubigeo != "" && (!zoneUbigeoPattern.MatchString(z.Ubigeo) || !strings.HasPrefix(z.Ubigeo, city.Ubigeo)):
		return Zone{}, ErrInvalidUbigeo
	case z.SortOrder < 0 || z.SortOrder > 32767:
		return Zone{}, ErrInvalidSortOrder
	}
	return z, nil
}

// Boundary es un límite ya validado: MultiPolygon GeoJSON en 2D y su rectángulo envolvente.
type Boundary struct {
	GeoJSON json.RawMessage
	Min     GeoPoint
	Max     GeoPoint
}

// Center es el centro del rectángulo envolvente (basta para saber si el límite está en su ciudad).
func (b Boundary) Center() GeoPoint {
	return GeoPoint{Lat: (b.Min.Lat + b.Max.Lat) / 2, Lng: (b.Min.Lng + b.Max.Lng) / 2}
}

type geoJSONObject struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
	Geometry    *geoJSONObject  `json:"geometry"`
	Features    []geoJSONObject `json:"features"`
}

// ParseBoundary acepta una geometría Polygon o MultiPolygon, un Feature con ella o un
// FeatureCollection de un solo Feature (lo que descarga overpass-turbo o geojson.io). Quita la
// altura y revisa anillos cerrados y coordenadas en rango; la base corrige autointersecciones.
func ParseBoundary(raw json.RawMessage) (Boundary, error) {
	var obj geoJSONObject
	if json.Unmarshal(raw, &obj) != nil {
		return Boundary{}, ErrInvalidBoundary
	}
	if obj.Type == "FeatureCollection" {
		if len(obj.Features) != 1 {
			return Boundary{}, ErrInvalidBoundary
		}
		obj = obj.Features[0]
	}
	if obj.Type == "Feature" {
		if obj.Geometry == nil {
			return Boundary{}, ErrInvalidBoundary
		}
		obj = *obj.Geometry
	}

	var polygons [][][][]float64
	switch obj.Type {
	case "Polygon":
		var p [][][]float64
		if json.Unmarshal(obj.Coordinates, &p) != nil {
			return Boundary{}, ErrInvalidBoundary
		}
		polygons = [][][][]float64{p}
	case "MultiPolygon":
		if json.Unmarshal(obj.Coordinates, &polygons) != nil {
			return Boundary{}, ErrInvalidBoundary
		}
	default:
		return Boundary{}, ErrInvalidBoundary
	}
	return buildBoundary(polygons)
}

func buildBoundary(polygons [][][][]float64) (Boundary, error) {
	b := Boundary{Min: GeoPoint{Lat: math.Inf(1), Lng: math.Inf(1)}, Max: GeoPoint{Lat: math.Inf(-1), Lng: math.Inf(-1)}}
	clean := make([][][][2]float64, 0, len(polygons))
	vertices := 0
	for _, polygon := range polygons {
		if len(polygon) == 0 {
			return Boundary{}, ErrInvalidBoundary
		}
		rings := make([][][2]float64, 0, len(polygon))
		for _, ring := range polygon {
			vertices += len(ring)
			if len(ring) < 4 || vertices > BoundaryMaxVertices {
				return Boundary{}, ErrInvalidBoundary
			}
			out := make([][2]float64, len(ring))
			for i, pos := range ring {
				if len(pos) < 2 {
					return Boundary{}, ErrInvalidBoundary
				}
				p := GeoPoint{Lng: pos[0], Lat: pos[1]}
				if p.Validate() != nil {
					return Boundary{}, ErrInvalidBoundary
				}
				b.Min = GeoPoint{Lat: min(b.Min.Lat, p.Lat), Lng: min(b.Min.Lng, p.Lng)}
				b.Max = GeoPoint{Lat: max(b.Max.Lat, p.Lat), Lng: max(b.Max.Lng, p.Lng)}
				out[i] = [2]float64{p.Lng, p.Lat}
			}
			if out[0] != out[len(out)-1] {
				return Boundary{}, ErrInvalidBoundary // anillo sin cerrar
			}
			rings = append(rings, out)
		}
		clean = append(clean, rings)
	}
	if len(clean) == 0 {
		return Boundary{}, ErrInvalidBoundary
	}
	geo, err := json.Marshal(map[string]any{"type": "MultiPolygon", "coordinates": clean})
	if err != nil {
		return Boundary{}, ErrInvalidBoundary
	}
	b.GeoJSON = geo
	return b, nil
}

// CheckZoneNearCity rechaza un límite que no cae cerca del centro de su ciudad.
func CheckZoneNearCity(b Boundary, city City) error {
	if DistanceMeters(city.Center, b.Center()) > ZoneMaxDistanceM {
		return ErrZoneFarFromCity
	}
	return nil
}

// CheckCityCanEnable: encender una ciudad sin distritos activos dejaría a sus usuarios sin zona.
func CheckCityCanEnable(zones []Zone) error {
	for _, z := range zones {
		if z.Enabled {
			return nil
		}
	}
	return ErrCityWithoutZones
}
