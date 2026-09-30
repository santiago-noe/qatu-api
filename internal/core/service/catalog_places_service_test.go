package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// squareBoundary: un cuadrado de 0,01° (≈1 km) alrededor del punto, en GeoJSON.
func squareBoundary(lat, lng float64) json.RawMessage {
	d := 0.01
	return json.RawMessage(fmt.Sprintf(`{"type":"Polygon","coordinates":[[[%g,%g],[%g,%g],[%g,%g],[%g,%g],[%g,%g]]]}`,
		lng-d, lat-d, lng+d, lat-d, lng+d, lat+d, lng-d, lat+d, lng-d, lat-d))
}

func TestAdminCreateCityAndEnable(t *testing.T) {
	svc, repo, _ := newCatalogAdminFixture()
	ctx := context.Background()

	cusco, err := svc.CreateCity(ctx, "admin-1", domain.City{Slug: "cusco", Name: " Cusco ", Region: "Cusco", Ubigeo: "0801",
		Center: domain.GeoPoint{Lat: -13.5167, Lng: -71.9781}, Enabled: true}, "")
	if err != nil || cusco.Name != "Cusco" || cusco.Enabled {
		t.Fatalf("una ciudad nueva nace apagada: %+v, %v", cusco, err)
	}
	if last := repo.audits[len(repo.audits)-1]; last.Action != domain.AuditCityCreated {
		t.Fatalf("el alta se audita: %+v", last)
	}
	if _, err := svc.CreateCity(ctx, "admin-1", domain.City{Slug: "cusco", Name: "Cusco", Region: "Cusco"}, ""); !errors.Is(err, domain.ErrCityTaken) {
		t.Fatalf("slug repetido: %v", err)
	}
	if _, err := svc.CreateCity(ctx, "admin-1", domain.City{Slug: "Cusco", Name: "Cusco", Region: "Cusco"}, ""); !errors.Is(err, domain.ErrInvalidSlug) {
		t.Fatalf("slug inválido: %v", err)
	}

	if _, err := svc.UpdateCity(ctx, "admin-1", "cusco", CityPatch{Enabled: ptr(true)}, ""); !errors.Is(err, domain.ErrCityWithoutZones) {
		t.Fatalf("sin distritos no se enciende: %v", err)
	}
	if _, err := svc.CreateZone(ctx, "admin-1", "cusco", domain.Zone{Slug: "wanchaq", Name: "Wanchaq", Ubigeo: "080108", Enabled: true}, ""); err != nil {
		t.Fatal(err)
	}
	city, err := svc.UpdateCity(ctx, "admin-1", "cusco", CityPatch{Enabled: ptr(true), Name: ptr("Cusco  ciudad")}, "")
	if err != nil || !city.Enabled || city.Name != "Cusco ciudad" {
		t.Fatalf("con un distrito activo ya se enciende: %+v, %v", city, err)
	}
}

func TestAdminZones(t *testing.T) {
	svc, repo, cache := newCatalogAdminFixture()
	ctx := context.Background()
	_ = cache.Set(ctx, CatalogCachePrefix+"zones:ayacucho", []byte("[]"), time.Hour)

	near := squareBoundary(-13.17, -74.20)
	z, err := svc.CreateZone(ctx, "admin-1", "ayacucho", domain.Zone{Slug: "carmen-alto", Name: "Carmen Alto", Ubigeo: "050104",
		Enabled: true, Boundary: near}, "")
	if err != nil || z.CityID != "city-1" || !z.HasBoundary {
		t.Fatalf("CreateZone = %+v, %v", z, err)
	}
	if _, ok, _ := cache.Get(ctx, CatalogCachePrefix+"zones:ayacucho"); ok {
		t.Fatal("un distrito nuevo limpia la caché del catálogo público")
	}
	saved := repo.zones["city-1|carmen-alto"].Boundary
	if !json.Valid(saved) || string(saved) == string(near) {
		t.Fatalf("se guarda el límite normalizado (MultiPolygon), no el original: %s", saved)
	}
	audit := repo.audits[len(repo.audits)-1]
	if audit.After.(map[string]any)["boundary_changed"] != true {
		t.Fatalf("la auditoría dice que hay límite, sin guardar el polígono: %+v", audit.After)
	}

	rejects := []struct {
		name string
		zone domain.Zone
		want error
	}{
		{"ubigeo de otra provincia", domain.Zone{Slug: "wanchaq", Name: "Wanchaq", Ubigeo: "080108"}, domain.ErrInvalidUbigeo},
		{"límite roto", domain.Zone{Slug: "roto", Name: "Roto", Boundary: json.RawMessage(`{"type":"Point"}`)}, domain.ErrInvalidBoundary},
		{"coordenadas invertidas", domain.Zone{Slug: "lejos", Name: "Lejos", Boundary: squareBoundary(-74.20, -13.17)}, domain.ErrZoneFarFromCity},
		{"slug repetido", domain.Zone{Slug: "carmen-alto", Name: "Otra"}, domain.ErrZoneTaken},
	}
	for _, tt := range rejects {
		if _, err := svc.CreateZone(ctx, "admin-1", "ayacucho", tt.zone, ""); !errors.Is(err, tt.want) {
			t.Errorf("%s: quiero %v, llegó %v", tt.name, tt.want, err)
		}
	}
	repo.overlap = 0.4
	if _, err := svc.CreateZone(ctx, "admin-1", "ayacucho", domain.Zone{Slug: "copia", Name: "Copia", Boundary: near}, ""); !errors.Is(err, domain.ErrZoneOverlap) {
		t.Fatalf("un distrito que repite otro: %v", err)
	}
	repo.overlap = 0.001 // solo se tocan en el borde
	if _, err := svc.UpdateZone(ctx, "admin-1", "ayacucho", "carmen-alto", ZonePatch{Boundary: near}, ""); err != nil {
		t.Fatalf("un vecino que comparte el borde: %v", err)
	}

	n := len(repo.audits)
	if _, err := svc.UpdateZone(ctx, "admin-1", "ayacucho", "carmen-alto", ZonePatch{Name: ptr("Carmen Alto")}, ""); err != nil || len(repo.audits) != n {
		t.Fatal("si nada cambia no se escribe ni se audita")
	}
	z, err = svc.UpdateZone(ctx, "admin-1", "ayacucho", "carmen-alto", ZonePatch{Enabled: ptr(false), SortOrder: ptr(6)}, "")
	if err != nil || z.Enabled || z.SortOrder != 6 || z.Boundary != nil {
		t.Fatalf("UpdateZone = %+v, %v", z, err)
	}
	if !repo.zones["city-1|carmen-alto"].HasBoundary || repo.zones["city-1|carmen-alto"].Boundary == nil {
		t.Fatal("sin límite en el cambio, el distrito conserva el suyo")
	}

	// Ayacucho está encendida y su último distrito activo es "ayacucho".
	if _, err := svc.UpdateZone(ctx, "admin-1", "ayacucho", "ayacucho", ZonePatch{Enabled: ptr(false)}, ""); !errors.Is(err, domain.ErrCityWithoutZones) {
		t.Fatalf("no se apaga el último distrito de una ciudad encendida: %v", err)
	}
	if _, err := svc.UpdateZone(ctx, "admin-1", "ayacucho", "no-existe", ZonePatch{}, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("distrito inexistente: %v", err)
	}
}

func TestAdminCategoryCities(t *testing.T) {
	svc, _, _ := newCatalogAdminFixture()
	ctx := context.Background()
	if err := svc.SetCategoryCityScope(ctx, "admin-1", "c1", "ayacucho", ptr(false), ""); err != nil {
		t.Fatal(err)
	}
	category, scopes, err := svc.CategoryCities(ctx, "c1")
	if err != nil || len(scopes) != 1 || scopes[0].ActiveFor(category) {
		t.Fatalf("apagada solo en Ayacucho: %+v, %v", scopes, err)
	}
	if _, _, err := svc.CategoryCities(ctx, "no-existe"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("categoría inexistente: %v", err)
	}
}
