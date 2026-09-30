package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestSettingsResolverSnapshot(t *testing.T) {
	repo := newMemoryCatalogAdmin() // comisión global de 1000 pb
	repo.settings[settingKey("rental.owner_commission_bps", "city-1", "construccion")] = domain.Setting{
		ID: "s2", Key: "rental.owner_commission_bps", CityID: "city-1", CategoryID: "construccion", Value: json.RawMessage("700"), Version: 3,
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	resolver := NewSettingsResolver(repo, &fakeClock{now: now})
	ctx := context.Background()
	rotomartillo := domain.Category{ID: "rotomartillo", ParentID: "construccion"}

	snap, err := resolver.Snapshot(ctx, "city-1", rotomartillo, "rental.owner_commission_bps")
	if err != nil || !snap.TakenAt.Equal(now) {
		t.Fatalf("Snapshot = %+v, %v", snap, err)
	}
	if v := snap.Values["rental.owner_commission_bps"]; v.SettingID != "s2" || v.Version != 3 {
		t.Fatalf("en Ayacucho aplica el ajuste de Construcción, con su versión: %+v", v)
	}
	if other, _ := resolver.Snapshot(ctx, "city-2", rotomartillo, "rental.owner_commission_bps"); other.Values["rental.owner_commission_bps"].SettingID != "s1" {
		t.Fatal("en otra ciudad aplica el global")
	}
	if _, err := resolver.Snapshot(ctx, "city-1", rotomartillo, "listings.public_radius_m"); !errors.Is(err, domain.ErrSettingMissing) {
		t.Fatalf("una clave sin valor global: %v", err)
	}
}
