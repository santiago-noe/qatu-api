package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// squareGeoJSON: un cuadrado de lado 2d grados con centro en (lat, lng), como MultiPolygon.
func squareGeoJSON(lat, lng, d float64) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"MultiPolygon","coordinates":[[[[%g,%g],[%g,%g],[%g,%g],[%g,%g],[%g,%g]]]]}`,
		lng-d, lat-d, lng+d, lat-d, lng+d, lat+d, lng-d, lat+d, lng-d, lat-d))
}

func TestCatalogPlacesRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewCatalogRepository(db)
	ctx := context.Background()
	audit := domain.AuditEntry{Action: domain.AuditCityCreated, Entity: "city", After: map[string]any{"test": true}}

	cusco := domain.City{Slug: "cusco", Name: "Cusco", Region: "Cusco", Ubigeo: "0801", Center: domain.GeoPoint{Lat: -13.5167, Lng: -71.9781}}
	cityID, err := repo.CreateCity(ctx, cusco, audit)
	if err != nil {
		t.Fatal(err)
	}
	cusco, err = repo.FindCityBySlug(ctx, "cusco")
	if err != nil || cusco.ID != cityID || cusco.Enabled || cusco.Ubigeo != "0801" || cusco.Timezone != "America/Lima" {
		t.Fatalf("la ciudad nace apagada y con la zona horaria de Perú: %+v, %v", cusco, err)
	}
	for _, dup := range []domain.City{
		{Slug: "cusco", Name: "Otra", Region: "Cusco", Center: cusco.Center},
		{Slug: "otra", Name: "Otra", Region: "Cusco", Ubigeo: "0801", Center: cusco.Center},
	} {
		if _, err := repo.CreateCity(ctx, dup, audit); !errors.Is(err, domain.ErrCityTaken) {
			t.Fatalf("slug o ubigeo repetido: %v", err)
		}
	}

	// Dos distritos vecinos: comparten el borde lng = -71.97.
	wanchaq := domain.Zone{CityID: cityID, Slug: "wanchaq", Name: "Wanchaq", Ubigeo: "080108", SortOrder: 1, Enabled: true,
		Boundary: squareGeoJSON(-13.52, -71.96, 0.01)}
	if _, err := repo.CreateZone(ctx, wanchaq, audit); err != nil {
		t.Fatal(err)
	}
	neighbour := squareGeoJSON(-13.52, -71.98, 0.01)
	t.Run("superposición", func(t *testing.T) {
		if r, err := repo.ZoneOverlap(ctx, cityID, "", wanchaq.Boundary); err != nil || r < 0.99 {
			t.Fatalf("el mismo límite se superpone por completo: %v, %v", r, err)
		}
		if r, err := repo.ZoneOverlap(ctx, cityID, "", neighbour); err != nil || r > 0.001 {
			t.Fatalf("un vecino que solo toca el borde: %v, %v", r, err)
		}
		w, _ := repo.FindZone(ctx, cityID, "wanchaq")
		if r, _ := repo.ZoneOverlap(ctx, cityID, w.ID, wanchaq.Boundary); r != 0 {
			t.Fatalf("al editar un distrito no se compara consigo mismo: %v", r)
		}
	})

	t.Run("rechazos", func(t *testing.T) {
		dup := wanchaq
		dup.Ubigeo = ""
		if _, err := repo.CreateZone(ctx, dup, audit); !errors.Is(err, domain.ErrZoneTaken) {
			t.Fatalf("slug repetido en la ciudad: %v", err)
		}
		// Un "polígono" con todos los puntos en una línea no tiene área.
		flat := domain.Zone{CityID: cityID, Slug: "plano", Name: "Plano", Enabled: true,
			Boundary: json.RawMessage(`{"type":"MultiPolygon","coordinates":[[[[-71.99,-13.5],[-71.98,-13.5],[-71.97,-13.5],[-71.99,-13.5]]]]}`)}
		if _, err := repo.CreateZone(ctx, flat, audit); !errors.Is(err, domain.ErrInvalidBoundary) {
			t.Fatalf("un límite sin área: %v", err)
		}
	})

	t.Run("lista con límite y detección", func(t *testing.T) {
		zones, err := repo.ListAllZones(ctx, cityID)
		if err != nil || len(zones) != 1 || !zones[0].HasBoundary {
			t.Fatalf("ListAllZones = %+v, %v", zones, err)
		}
		var geo struct{ Type string }
		if json.Unmarshal(zones[0].Boundary, &geo) != nil || geo.Type != "MultiPolygon" {
			t.Fatalf("el límite simplificado sigue siendo MultiPolygon: %s", zones[0].Boundary)
		}
		if _, err := repo.LocationAt(ctx, domain.GeoPoint{Lat: -13.52, Lng: -71.96}); !errors.Is(err, domain.ErrOutOfCoverage) {
			t.Fatalf("una ciudad apagada no detecta: %v", err)
		}
		cusco.Enabled = true
		if err := repo.UpdateCity(ctx, cusco, audit); err != nil {
			t.Fatal(err)
		}
		loc, err := repo.LocationAt(ctx, domain.GeoPoint{Lat: -13.52, Lng: -71.96})
		if err != nil || loc.Zone.Slug != "wanchaq" || loc.City.Slug != "cusco" {
			t.Fatalf("encendida, ya detecta su distrito: %+v, %v", loc, err)
		}
	})

	t.Run("editar conserva o cambia el límite", func(t *testing.T) {
		w, _ := repo.FindZone(ctx, cityID, "wanchaq")
		w.Name, w.SortOrder = "Wánchaq", 2
		if err := repo.UpdateZone(ctx, w, audit); err != nil {
			t.Fatal(err)
		}
		if got, _ := repo.FindZone(ctx, cityID, "wanchaq"); got.Name != "Wánchaq" || !got.HasBoundary {
			t.Fatalf("sin límite en el cambio conserva el suyo: %+v", got)
		}
		w.Boundary = neighbour
		if err := repo.UpdateZone(ctx, w, audit); err != nil {
			t.Fatal(err)
		}
		if loc, _ := repo.LocationAt(ctx, domain.GeoPoint{Lat: -13.52, Lng: -71.98}); loc.Zone.Slug != "wanchaq" {
			t.Fatal("con el límite nuevo detecta en el lugar nuevo")
		}
		if _, err := repo.FindZone(ctx, cityID, "no-existe"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("FindZone inexistente: %v", err)
		}
	})

	t.Run("alcance por ciudad", func(t *testing.T) {
		var categoryID string
		must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'rotomartillo'`).Scan(&categoryID))
		off := false
		must(t, repo.SetCategoryCityScope(ctx, categoryID, cityID, &off, audit))
		scopes, err := repo.CategoryCityScopes(ctx, categoryID)
		if err != nil || len(scopes) != 2 {
			t.Fatalf("todas las ciudades, también las apagadas: %+v, %v", scopes, err)
		}
		for _, s := range scopes {
			switch s.City.Slug {
			case "ayacucho":
				if s.Override != nil {
					t.Fatal("Ayacucho sigue el valor global")
				}
			case "cusco":
				if s.Override == nil || *s.Override {
					t.Fatal("en Cusco está apagada")
				}
			}
		}
		if _, err := repo.CategoryCityScopes(ctx, "no-es-uuid"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("un ID que no es UUID: %v", err)
		}
	})
}
