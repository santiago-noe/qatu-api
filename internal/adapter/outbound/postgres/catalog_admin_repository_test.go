package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestCatalogAdminRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewCatalogRepository(db)
	ctx := context.Background()
	audit := func(action string) domain.AuditEntry {
		return domain.AuditEntry{Action: action, Entity: "category", After: map[string]any{"test": true}}
	}

	all, err := repo.ListAllCategories(ctx, domain.VerticalRental)
	if err != nil || len(all) != 28 {
		t.Fatalf("el admin ve también lo apagado (6 raíces + 22 tipos): %d %v", len(all), err)
	}
	construccion := all[0]

	var createdID string
	t.Run("crear un tipo bajo una raíz", func(t *testing.T) {
		c := domain.Category{Vertical: domain.VerticalRental, ParentID: construccion.ID, Slug: "cortadora-de-cemento",
			Name: "Cortadora de cemento", AttributesSchema: json.RawMessage(`{"type":"object"}`), RiskLevel: domain.RiskHigh, Enabled: true}
		id, err := repo.CreateCategory(ctx, c, audit(domain.AuditCategoryCreated))
		if err != nil || id == "" {
			t.Fatalf("CreateCategory = %q, %v", id, err)
		}
		createdID = id
		got, err := repo.FindCategory(ctx, id)
		if err != nil || got.Name != "Cortadora de cemento" || got.ParentID != construccion.ID || got.RiskLevel != domain.RiskHigh {
			t.Fatalf("FindCategory = %+v, %v", got, err)
		}
	})

	t.Run("rechazos de la base traducidos", func(t *testing.T) {
		dup := domain.Category{Vertical: domain.VerticalRental, Slug: "construccion", Name: "X",
			AttributesSchema: json.RawMessage(`{}`), RiskLevel: domain.RiskLow}
		if _, err := repo.CreateCategory(ctx, dup, audit(domain.AuditCategoryCreated)); !errors.Is(err, domain.ErrSlugTaken) {
			t.Fatalf("slug repetido en la vertical: %v", err)
		}
		third := dup
		third.Slug, third.ParentID = "tercer-nivel", createdID
		if _, err := repo.CreateCategory(ctx, third, audit(domain.AuditCategoryCreated)); !errors.Is(err, domain.ErrCategoryTree) {
			t.Fatalf("tercer nivel: %v", err)
		}
		orphan := dup
		orphan.Slug, orphan.ParentID = "huerfana", "00000000-0000-0000-0000-000000000000"
		if _, err := repo.CreateCategory(ctx, orphan, audit(domain.AuditCategoryCreated)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("padre inexistente: %v", err)
		}
		if _, err := repo.FindCategory(ctx, "no-es-uuid"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("un ID que no es UUID no existe: %v", err)
		}
	})

	t.Run("editar", func(t *testing.T) {
		c, _ := repo.FindCategory(ctx, createdID)
		c.Name, c.Enabled = "Cortadora de concreto", false
		if err := repo.UpdateCategory(ctx, c, audit(domain.AuditCategoryUpdated)); err != nil {
			t.Fatal(err)
		}
		if got, _ := repo.FindCategory(ctx, createdID); got.Name != "Cortadora de concreto" || got.Enabled {
			t.Fatalf("UpdateCategory no guardó: %+v", got)
		}
		c.ID = "00000000-0000-0000-0000-000000000000"
		if err := repo.UpdateCategory(ctx, c, audit(domain.AuditCategoryUpdated)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("editar una categoría inexistente: %v", err)
		}
	})

	t.Run("ciudad y alcance por ciudad", func(t *testing.T) {
		city, err := repo.FindCityBySlug(ctx, "ayacucho")
		if err != nil {
			t.Fatal(err)
		}
		on := true
		if err := repo.SetCategoryCityScope(ctx, createdID, city.ID, &on, audit(domain.AuditCategoryCityScope)); err != nil {
			t.Fatal(err)
		}
		visible, _ := repo.ListCategories(ctx, domain.VerticalRental, city.ID)
		if !containsSlug(visible, "cortadora-de-cemento") {
			t.Fatal("encendida solo en Ayacucho, aunque esté apagada en general")
		}
		if err := repo.SetCategoryCityScope(ctx, createdID, city.ID, nil, audit(domain.AuditCategoryCityScope)); err != nil {
			t.Fatal(err)
		}
		visible, _ = repo.ListCategories(ctx, domain.VerticalRental, city.ID)
		if containsSlug(visible, "cortadora-de-cemento") {
			t.Fatal("sin ajuste vuelve a su valor global (apagada)")
		}

		if err := repo.SetCityEnabled(ctx, city.ID, false, audit(domain.AuditCityUpdated)); err != nil {
			t.Fatal(err)
		}
		if cities, _ := repo.ListCities(ctx); len(cities) != 0 {
			t.Fatal("una ciudad apagada no se lista")
		}
		if again, _ := repo.FindCityBySlug(ctx, "ayacucho"); again.Enabled {
			t.Fatal("el admin la sigue encontrando, apagada")
		}
	})

	t.Run("ajustes con versión e historial", func(t *testing.T) {
		before, err := repo.FindSetting(ctx, "rental.owner_commission_bps", "", "")
		if err != nil || string(before.Value) != "1000" {
			t.Fatalf("FindSetting = %+v, %v", before, err)
		}
		entry := domain.AuditEntry{Action: domain.AuditSettingChanged, Entity: "platform_setting",
			Before: map[string]any{"key": before.Key, "value": 1000}, After: map[string]any{"key": before.Key, "value": 900}, IP: "190.1.2.3"}
		s := before
		s.Value = json.RawMessage("900")
		saved, err := repo.UpsertSetting(ctx, s, entry)
		if err != nil || saved.ID != before.ID || saved.Version != before.Version+1 || string(saved.Value) != "900" {
			t.Fatalf("UpsertSetting = %+v, %v", saved, err)
		}
		history, err := repo.SettingHistory(ctx, "rental.owner_commission_bps", 10)
		if err != nil || len(history) != 1 || history[0].IP != "190.1.2.3" {
			t.Fatalf("SettingHistory = %+v, %v", history, err)
		}
		if _, err := repo.FindSetting(ctx, "rental.owner_commission_bps", "no-es-uuid", ""); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("un alcance inválido no existe: %v", err)
		}
		list, _ := repo.ListSettings(ctx)
		if len(list) != 4 {
			t.Fatalf("4 ajustes del piloto: %d", len(list))
		}
	})
}

func containsSlug(list []domain.Category, slug string) bool {
	for _, c := range list {
		if c.Slug == slug {
			return true
		}
	}
	return false
}
