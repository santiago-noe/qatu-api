package service

import (
	"context"
	"errors"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func newLocationFixture() (*UserLocationService, *memoryAccounts) {
	accounts := newMemoryAccounts()
	accounts.users["u-ana"] = domain.User{ID: "u-ana", Status: domain.UserActive}
	catalog, _, _ := newCatalogFixture() // Ayacucho (city-1) con el distrito carmen-alto (z1)
	return NewUserLocationService(accounts, catalog), accounts
}

func TestUserLocationSetAndGet(t *testing.T) {
	svc, accounts := newLocationFixture()
	ctx := context.Background()

	if _, ok, err := svc.Get(ctx, "u-ana"); ok || err != nil {
		t.Fatalf("sin elegir no hay ubicación: ok=%v err=%v", ok, err)
	}

	loc, err := svc.Set(ctx, "u-ana", "ayacucho", "carmen-alto", "190.1.2.3")
	if err != nil || loc.City.Slug != "ayacucho" || loc.Zone.Slug != "carmen-alto" {
		t.Fatalf("Set = %+v, %v", loc, err)
	}
	if u := accounts.users["u-ana"]; u.CityID != "city-1" || u.ZoneID != "z1" {
		t.Fatalf("se guardan los IDs: %+v", u)
	}
	if last := accounts.audits[len(accounts.audits)-1]; last.Action != domain.AuditLocationUpdated {
		t.Fatalf("el cambio se audita: %+v", last)
	}

	got, ok, err := svc.Get(ctx, "u-ana")
	if err != nil || !ok || got.Zone.Slug != "carmen-alto" || got.City.Name != "Ayacucho" {
		t.Fatalf("Get = %+v %v %v", got, ok, err)
	}

	n := len(accounts.audits)
	if _, err := svc.Set(ctx, "u-ana", "ayacucho", "carmen-alto", ""); err != nil || len(accounts.audits) != n {
		t.Fatal("elegir lo mismo no escribe ni audita")
	}
}

func TestUserLocationRejectsUnknown(t *testing.T) {
	svc, _ := newLocationFixture()
	ctx := context.Background()
	if _, err := svc.Set(ctx, "u-ana", "cusco", "wanchaq", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("una ciudad no habilitada: %v", err)
	}
	if _, err := svc.Set(ctx, "u-ana", "ayacucho", "miraflores", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("un distrito que no es de la ciudad: %v", err)
	}
}

func TestUserLocationDisabledZone(t *testing.T) {
	svc, accounts := newLocationFixture()
	// Guardó un distrito que luego se deshabilitó: ya no aparece en el catálogo.
	accounts.users["u-ana"] = domain.User{ID: "u-ana", CityID: "city-1", ZoneID: "zona-apagada"}
	if _, ok, err := svc.Get(context.Background(), "u-ana"); ok || err != nil {
		t.Fatalf("debe volver a elegir: ok=%v err=%v", ok, err)
	}
}
