package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Usa los datos reales del piloto (migraciones 0004 y 0005): polígonos del INEI vía OSM.
func TestCatalogRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewCatalogRepository(db)
	ctx := context.Background()

	cities, err := repo.ListCities(ctx)
	if err != nil || len(cities) != 1 || cities[0].Slug != "ayacucho" || cities[0].Center.Lat > -13 {
		t.Fatalf("ListCities = %+v, %v", cities, err)
	}
	city := cities[0]

	t.Run("zonas del piloto en orden", func(t *testing.T) {
		zones, err := repo.ListZones(ctx, city.ID)
		if err != nil || len(zones) != 5 || zones[0].Slug != "ayacucho" || !zones[4].HasBoundary {
			t.Fatalf("ListZones = %+v, %v", zones, err)
		}
	})

	t.Run("detección de distrito por punto", func(t *testing.T) {
		loc, err := repo.LocationAt(ctx, domain.GeoPoint{Lat: -13.1631, Lng: -74.2237}) // Plaza de Armas
		if err != nil || loc.Zone.Slug != "ayacucho" || loc.City.Slug != "ayacucho" {
			t.Fatalf("LocationAt = %+v, %v", loc, err)
		}
		if _, err := repo.LocationAt(ctx, domain.GeoPoint{Lat: -12.0464, Lng: -77.0428}); !errors.Is(err, domain.ErrOutOfCoverage) {
			t.Fatalf("Lima está fuera de cobertura, llegó %v", err)
		}
	})

	t.Run("categorías de alquiler: sin la vertical apagada, con esquema y riesgo", func(t *testing.T) {
		flat, err := repo.ListCategories(ctx, domain.VerticalRental, "")
		if err != nil {
			t.Fatal(err)
		}
		tree := domain.BuildCategoryTree(flat)
		if len(tree) != 5 || tree[0].Slug != "construccion" {
			t.Fatalf("5 categorías del piloto, Eventos queda apagada: %+v", tree)
		}
		if !json.Valid(tree[0].AttributesSchema) || len(tree[0].Children) != 5 {
			t.Fatalf("construcción trae su esquema y 5 tipos: %+v", tree[0])
		}
		risks := map[string]domain.RiskLevel{}
		for _, root := range tree {
			for _, c := range root.Children {
				risks[c.Slug] = c.RiskLevel
			}
		}
		if risks["motosierra"] != domain.RiskHigh || risks["escaleras"] != domain.RiskLow || risks["rotomartillo"] != domain.RiskMedium {
			t.Fatalf("riesgos de la migración 0005: %v", risks)
		}
	})

	t.Run("ajuste por ciudad y categorías prohibidas", func(t *testing.T) {
		// Encender Eventos solo en Ayacucho y prohibir Jardín.
		if _, err := db.Pool.Exec(ctx, `
			INSERT INTO category_city_overrides (category_id, city_id, enabled)
			SELECT id, $1, true FROM categories WHERE vertical = 'rental' AND slug IN ('eventos-y-audiovisual', 'proyector')`, city.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `UPDATE categories SET prohibited = true WHERE vertical = 'rental' AND slug = 'jardin'`); err != nil {
			t.Fatal(err)
		}
		inCity, _ := repo.ListCategories(ctx, domain.VerticalRental, city.ID)
		global, _ := repo.ListCategories(ctx, domain.VerticalRental, "")
		has := func(list []domain.Category, slug string) bool {
			for _, c := range list {
				if c.Slug == slug {
					return true
				}
			}
			return false
		}
		if !has(inCity, "eventos-y-audiovisual") || !has(inCity, "proyector") || has(global, "eventos-y-audiovisual") {
			t.Fatal("el ajuste por ciudad solo aplica en esa ciudad")
		}
		if has(inCity, "jardin") || has(global, "jardin") {
			t.Fatal("una categoría prohibida no se muestra en ninguna ciudad")
		}
	})

	t.Run("oficios", func(t *testing.T) {
		flat, err := repo.ListCategories(ctx, domain.VerticalService, "")
		if err != nil || len(flat) != 10 || flat[0].Slug != "gasfiteria" {
			t.Fatalf("10 oficios en orden: %d %v", len(flat), err)
		}
	})
}
