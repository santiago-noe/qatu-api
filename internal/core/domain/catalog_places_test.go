package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

var huamanga = City{Slug: "ayacucho", Name: "Ayacucho", Region: "Ayacucho", Ubigeo: "0501",
	Center: GeoPoint{Lat: -13.1631, Lng: -74.2236}}

func TestNormalizeCity(t *testing.T) {
	c, err := NormalizeCity(City{Slug: "cusco", Name: "  Cusco ", Region: "Cusco  Región", Ubigeo: "0801",
		Center: GeoPoint{Lat: -13.5167, Lng: -71.9781}})
	if err != nil || c.Name != "Cusco" || c.Region != "Cusco Región" {
		t.Fatalf("NormalizeCity = %+v, %v", c, err)
	}
	tests := []struct {
		name   string
		change func(*City)
		want   error
	}{
		{"slug", func(c *City) { c.Slug = "Cusco" }, ErrInvalidSlug},
		{"sin nombre", func(c *City) { c.Name = " " }, ErrInvalidName},
		{"sin región", func(c *City) { c.Region = "" }, ErrInvalidName},
		{"ubigeo de distrito", func(c *City) { c.Ubigeo = "080101" }, ErrInvalidUbigeo},
		{"centro fuera del mundo", func(c *City) { c.Center.Lat = -91 }, ErrInvalidLocation},
	}
	for _, tt := range tests {
		c := huamanga
		tt.change(&c)
		if _, err := NormalizeCity(c); !errors.Is(err, tt.want) {
			t.Errorf("%s: quiero %v, llegó %v", tt.name, tt.want, err)
		}
	}
}

func TestNormalizeZone(t *testing.T) {
	z, err := NormalizeZone(Zone{Slug: "carmen-alto", Name: " Carmen  Alto", Ubigeo: "050104"}, huamanga)
	if err != nil || z.Name != "Carmen Alto" {
		t.Fatalf("NormalizeZone = %+v, %v", z, err)
	}
	if _, err := NormalizeZone(Zone{Slug: "wanchaq", Name: "Wanchaq", Ubigeo: "080108"}, huamanga); !errors.Is(err, ErrInvalidUbigeo) {
		t.Fatal("el ubigeo del distrito empieza con el de su provincia")
	}
	if _, err := NormalizeZone(Zone{Slug: "x", Name: "X", SortOrder: -1}, huamanga); !errors.Is(err, ErrInvalidSortOrder) {
		t.Fatal("orden negativo")
	}
	if _, err := NormalizeZone(Zone{Slug: "sin-ubigeo", Name: "Barrio"}, huamanga); err != nil {
		t.Fatalf("el ubigeo es opcional (barrios): %v", err)
	}
}

// square es un cuadrado de lado d grados alrededor del punto, como anillo GeoJSON (lng, lat).
func square(center GeoPoint, d float64) string {
	w, e, s, n := center.Lng-d, center.Lng+d, center.Lat-d, center.Lat+d
	return fmt.Sprintf(`[[%g,%g],[%g,%g],[%g,%g],[%g,%g],[%g,%g]]`, w, s, e, s, e, n, w, n, w, s)
}

func TestParseBoundary(t *testing.T) {
	ring := square(huamanga.Center, 0.01)
	polygon := `{"type":"Polygon","coordinates":[` + ring + `]}`
	for name, raw := range map[string]string{
		"geometría":        polygon,
		"multipolígono":    `{"type":"MultiPolygon","coordinates":[[` + ring + `]]}`,
		"feature":          `{"type":"Feature","properties":{"name":"Ayacucho"},"geometry":` + polygon + `}`,
		"colección de uno": `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":` + polygon + `}]}`,
		"con altura (3D)":  `{"type":"Polygon","coordinates":[[[-74.23,-13.17,2750],[-74.21,-13.17,2760],[-74.21,-13.15,2740],[-74.23,-13.17,2750]]]}`,
	} {
		b, err := ParseBoundary(json.RawMessage(raw))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.HasPrefix(string(b.GeoJSON), `{"coordinates":[[[[`) || !strings.Contains(string(b.GeoJSON), `"type":"MultiPolygon"`) {
			t.Errorf("%s: siempre sale un MultiPolygon 2D: %s", name, b.GeoJSON)
		}
		if strings.Contains(string(b.GeoJSON), "2750") {
			t.Errorf("%s: la altura se descarta", name)
		}
	}

	b, _ := ParseBoundary(json.RawMessage(polygon))
	if c := b.Center(); DistanceMeters(c, huamanga.Center) > 1 {
		t.Fatalf("centro del rectángulo = %+v", c)
	}

	for name, raw := range map[string]string{
		"no es JSON":            `{"type":`,
		"un punto":              `{"type":"Point","coordinates":[-74.2,-13.1]}`,
		"colección de dos":      `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":` + polygon + `},{"type":"Feature","geometry":` + polygon + `}]}`,
		"feature sin geometría": `{"type":"Feature","geometry":null}`,
		"anillo abierto":        `{"type":"Polygon","coordinates":[[[-74.23,-13.17],[-74.21,-13.17],[-74.21,-13.15],[-74.23,-13.15]]]}`,
		"anillo de 3 puntos":    `{"type":"Polygon","coordinates":[[[-74.23,-13.17],[-74.21,-13.17],[-74.23,-13.17]]]}`,
		"latitud imposible":     `{"type":"Polygon","coordinates":[[[-13.1,-94.2],[-13.2,-94.2],[-13.2,-94.3],[-13.1,-94.2]]]}`,
		"multipolígono vacío":   `{"type":"MultiPolygon","coordinates":[]}`,
	} {
		if _, err := ParseBoundary(json.RawMessage(raw)); !errors.Is(err, ErrInvalidBoundary) {
			t.Errorf("%s: quiero ErrInvalidBoundary, llegó %v", name, err)
		}
	}

	var huge strings.Builder
	huge.WriteString(`{"type":"Polygon","coordinates":[[`)
	for i := range BoundaryMaxVertices {
		fmt.Fprintf(&huge, "[%g,-13.1],", -74.2+float64(i)*1e-7)
	}
	huge.WriteString(`[-74.2,-13.1]]]}`)
	if _, err := ParseBoundary(json.RawMessage(huge.String())); !errors.Is(err, ErrInvalidBoundary) {
		t.Fatal("un límite con demasiados vértices se rechaza")
	}
}

func TestCheckZoneNearCity(t *testing.T) {
	near, _ := ParseBoundary(json.RawMessage(`{"type":"Polygon","coordinates":[` + square(huamanga.Center, 0.02) + `]}`))
	if err := CheckZoneNearCity(near, huamanga); err != nil {
		t.Fatal(err)
	}
	// Latitud y longitud invertidas: el punto cae en otro continente.
	swapped, _ := ParseBoundary(json.RawMessage(`{"type":"Polygon","coordinates":[` + square(GeoPoint{Lat: -74.2236, Lng: -13.1631}, 0.02) + `]}`))
	if err := CheckZoneNearCity(swapped, huamanga); !errors.Is(err, ErrZoneFarFromCity) {
		t.Fatalf("coordenadas invertidas: %v", err)
	}
}

func TestCheckCityCanEnable(t *testing.T) {
	if err := CheckCityCanEnable(nil); !errors.Is(err, ErrCityWithoutZones) {
		t.Fatal("sin distritos no se enciende")
	}
	if err := CheckCityCanEnable([]Zone{{Enabled: false}}); !errors.Is(err, ErrCityWithoutZones) {
		t.Fatal("con distritos apagados tampoco")
	}
	if err := CheckCityCanEnable([]Zone{{Enabled: false}, {Enabled: true}}); err != nil {
		t.Fatal(err)
	}
}

func TestCategoryCityScopeActiveFor(t *testing.T) {
	on, off := true, false
	cat := Category{Enabled: false}
	if (CategoryCityScope{}).ActiveFor(cat) || !(CategoryCityScope{Override: &on}).ActiveFor(cat) {
		t.Fatal("sin ajuste sigue el valor global; con ajuste manda la ciudad")
	}
	if (CategoryCityScope{Override: &off}).ActiveFor(Category{Enabled: true}) {
		t.Fatal("apagada solo en esa ciudad")
	}
	if (CategoryCityScope{Override: &on}).ActiveFor(Category{Enabled: true, Prohibited: true}) {
		t.Fatal("una prohibida no se activa en ninguna ciudad")
	}
}
