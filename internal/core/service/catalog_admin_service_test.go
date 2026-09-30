package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// memoryCatalogAdmin guarda categorías, ciudades y ajustes en memoria, y cuenta las auditorías.
type memoryCatalogAdmin struct {
	categories map[string]domain.Category
	cities     map[string]domain.City
	scopes     map[string]*bool
	zones      map[string]domain.Zone // ciudad|slug
	settings   map[string]domain.Setting
	audits     []domain.AuditEntry
	nextID     int
	overlap    float64 // lo que responde ZoneOverlap
}

func newMemoryCatalogAdmin() *memoryCatalogAdmin {
	m := &memoryCatalogAdmin{
		categories: map[string]domain.Category{}, scopes: map[string]*bool{}, settings: map[string]domain.Setting{},
		cities: map[string]domain.City{"ayacucho": {ID: "city-1", Slug: "ayacucho", Name: "Ayacucho", Region: "Ayacucho",
			Ubigeo: "0501", Center: domain.GeoPoint{Lat: -13.1631, Lng: -74.2236}, Enabled: true}},
		zones: map[string]domain.Zone{"city-1|ayacucho": {ID: "z1", CityID: "city-1", Slug: "ayacucho", Name: "Ayacucho",
			Ubigeo: "050101", Enabled: true, HasBoundary: true}},
	}
	m.categories["c1"] = domain.Category{ID: "c1", Vertical: domain.VerticalRental, Slug: "construccion", Name: "Construcción",
		AttributesSchema: json.RawMessage(`{"type":"object"}`), RiskLevel: domain.RiskMedium, Enabled: true}
	m.settings[settingKey("rental.owner_commission_bps", "", "")] = domain.Setting{ID: "s1", Key: "rental.owner_commission_bps",
		Value: json.RawMessage("1000"), Version: 1}
	return m
}

func settingKey(key, city, category string) string { return key + "|" + city + "|" + category }

func (m *memoryCatalogAdmin) ListAllCategories(_ context.Context, v domain.Vertical) ([]domain.Category, error) {
	var out []domain.Category
	for _, c := range m.categories {
		if c.Vertical == v {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memoryCatalogAdmin) FindCategory(_ context.Context, id string) (domain.Category, error) {
	c, ok := m.categories[id]
	if !ok {
		return domain.Category{}, domain.ErrNotFound
	}
	return c, nil
}

func (m *memoryCatalogAdmin) CreateCategory(_ context.Context, c domain.Category, a domain.AuditEntry) (string, error) {
	for _, other := range m.categories {
		if other.Vertical == c.Vertical && other.Slug == c.Slug {
			return "", domain.ErrSlugTaken
		}
	}
	m.nextID++
	c.ID = fmt.Sprintf("new-%d", m.nextID)
	m.categories[c.ID] = c
	m.audits = append(m.audits, a)
	return c.ID, nil
}

func (m *memoryCatalogAdmin) UpdateCategory(_ context.Context, c domain.Category, a domain.AuditEntry) error {
	m.categories[c.ID] = c
	m.audits = append(m.audits, a)
	return nil
}

func (m *memoryCatalogAdmin) ListAllCities(context.Context) ([]domain.City, error) {
	return slices.Collect(maps.Values(m.cities)), nil
}

func (m *memoryCatalogAdmin) FindCityBySlug(_ context.Context, slug string) (domain.City, error) {
	c, ok := m.cities[slug]
	if !ok {
		return domain.City{}, domain.ErrNotFound
	}
	return c, nil
}

func (m *memoryCatalogAdmin) CreateCity(_ context.Context, c domain.City, a domain.AuditEntry) (string, error) {
	if _, ok := m.cities[c.Slug]; ok {
		return "", domain.ErrCityTaken
	}
	m.nextID++
	c.ID = fmt.Sprintf("city-new-%d", m.nextID)
	m.cities[c.Slug] = c
	m.audits = append(m.audits, a)
	return c.ID, nil
}

func (m *memoryCatalogAdmin) UpdateCity(_ context.Context, c domain.City, a domain.AuditEntry) error {
	m.cities[c.Slug] = c
	m.audits = append(m.audits, a)
	return nil
}

func (m *memoryCatalogAdmin) SetCategoryCityScope(_ context.Context, categoryID, cityID string, enabled *bool, a domain.AuditEntry) error {
	m.scopes[categoryID+"|"+cityID] = enabled
	m.audits = append(m.audits, a)
	return nil
}

func (m *memoryCatalogAdmin) CategoryCityScopes(_ context.Context, categoryID string) ([]domain.CategoryCityScope, error) {
	var out []domain.CategoryCityScope
	for _, c := range m.cities {
		out = append(out, domain.CategoryCityScope{City: c, Override: m.scopes[categoryID+"|"+c.ID]})
	}
	return out, nil
}

func (m *memoryCatalogAdmin) ListAllZones(_ context.Context, cityID string) ([]domain.Zone, error) {
	var out []domain.Zone
	for _, z := range m.zones {
		if z.CityID == cityID {
			out = append(out, z)
		}
	}
	return out, nil
}

func (m *memoryCatalogAdmin) FindZone(_ context.Context, cityID, slug string) (domain.Zone, error) {
	z, ok := m.zones[cityID+"|"+slug]
	if !ok {
		return domain.Zone{}, domain.ErrNotFound
	}
	z.Boundary = nil
	return z, nil
}

func (m *memoryCatalogAdmin) CreateZone(_ context.Context, z domain.Zone, a domain.AuditEntry) (string, error) {
	if _, ok := m.zones[z.CityID+"|"+z.Slug]; ok {
		return "", domain.ErrZoneTaken
	}
	m.nextID++
	z.ID = fmt.Sprintf("zone-%d", m.nextID)
	z.HasBoundary = z.Boundary != nil
	m.zones[z.CityID+"|"+z.Slug] = z
	m.audits = append(m.audits, a)
	return z.ID, nil
}

func (m *memoryCatalogAdmin) UpdateZone(_ context.Context, z domain.Zone, a domain.AuditEntry) error {
	k := z.CityID + "|" + z.Slug
	if z.Boundary == nil {
		z.Boundary = m.zones[k].Boundary
	}
	m.zones[k] = z
	m.audits = append(m.audits, a)
	return nil
}

func (m *memoryCatalogAdmin) ZoneOverlap(context.Context, string, string, json.RawMessage) (float64, error) {
	return m.overlap, nil
}

func (m *memoryCatalogAdmin) ListSettings(context.Context) ([]domain.Setting, error) {
	var out []domain.Setting
	for _, s := range m.settings {
		out = append(out, s)
	}
	return out, nil
}

func (m *memoryCatalogAdmin) FindSetting(_ context.Context, key, city, category string) (domain.Setting, error) {
	s, ok := m.settings[settingKey(key, city, category)]
	if !ok {
		return domain.Setting{}, domain.ErrNotFound
	}
	return s, nil
}

func (m *memoryCatalogAdmin) UpsertSetting(_ context.Context, s domain.Setting, a domain.AuditEntry) (domain.Setting, error) {
	k := settingKey(s.Key, s.CityID, s.CategoryID)
	s.Version = m.settings[k].Version + 1
	s.UpdatedAt = time.Now()
	m.settings[k] = s
	m.audits = append(m.audits, a)
	return s, nil
}

func (m *memoryCatalogAdmin) SettingHistory(context.Context, string, int) ([]domain.SettingChange, error) {
	return nil, nil
}

// stubSchemas acepta esquemas de objeto; rechaza el resto (el adaptador real tiene sus pruebas).
type stubSchemas struct{}

func (stubSchemas) CheckSchema(s json.RawMessage) error {
	if !bytes.Contains(s, []byte(`"object"`)) {
		return domain.ErrInvalidSchema
	}
	return nil
}

func newCatalogAdminFixture() (*CatalogAdminService, *memoryCatalogAdmin, *memoryCache) {
	repo := newMemoryCatalogAdmin()
	cache := newMemoryCache()
	return NewCatalogAdminService(repo, stubSchemas{}, cache), repo, cache
}

func ptr[T any](v T) *T { return &v }

func TestAdminCreateCategory(t *testing.T) {
	svc, repo, cache := newCatalogAdminFixture()
	ctx := context.Background()
	_ = cache.Set(ctx, CatalogCachePrefix+"categories:rental:", []byte("[]"), time.Hour)

	c, err := svc.CreateCategory(ctx, "admin-1", domain.Category{Vertical: domain.VerticalRental, ParentID: "c1",
		Slug: "cortadora", Name: " Cortadora  de cemento ", RiskLevel: domain.RiskHigh, Enabled: true}, "190.1.2.3")
	if err != nil || c.Name != "Cortadora de cemento" || string(c.AttributesSchema) == "" {
		t.Fatalf("CreateCategory = %+v, %v", c, err)
	}
	if last := repo.audits[len(repo.audits)-1]; last.Action != domain.AuditCategoryCreated || last.ActorID != "admin-1" {
		t.Fatalf("la creación se audita: %+v", last)
	}
	if _, ok, _ := cache.Get(ctx, CatalogCachePrefix+"categories:rental:"); ok {
		t.Fatal("crear limpia la caché del catálogo público")
	}

	bad := domain.Category{Vertical: domain.VerticalRental, Slug: "x", Name: "X", RiskLevel: domain.RiskLow,
		AttributesSchema: json.RawMessage(`{"type":"string"}`)}
	if _, err := svc.CreateCategory(ctx, "admin-1", bad, ""); !errors.Is(err, domain.ErrInvalidSchema) {
		t.Fatalf("un esquema que no es de objeto se rechaza: %v", err)
	}
	if _, err := svc.CreateCategory(ctx, "admin-1", domain.Category{Vertical: domain.VerticalRental, Slug: "Mal Slug", Name: "X", RiskLevel: domain.RiskLow}, ""); !errors.Is(err, domain.ErrInvalidSlug) {
		t.Fatalf("un slug inválido se rechaza antes de escribir: %v", err)
	}
}

func TestAdminUpdateCategory(t *testing.T) {
	svc, repo, _ := newCatalogAdminFixture()
	ctx := context.Background()

	c, err := svc.UpdateCategory(ctx, "admin-1", "c1", CategoryPatch{Prohibited: ptr(true), RiskLevel: ptr(domain.RiskHigh)}, "")
	if err != nil || !c.Prohibited || c.RiskLevel != domain.RiskHigh || c.Name != "Construcción" {
		t.Fatalf("solo cambian los campos enviados: %+v %v", c, err)
	}
	audit := repo.audits[len(repo.audits)-1]
	if audit.Action != domain.AuditCategoryUpdated || audit.Before.(map[string]any)["prohibited"] != false {
		t.Fatalf("se audita el antes y el después: %+v", audit)
	}

	n := len(repo.audits)
	// Mismo esquema con otro formato: no es un cambio.
	if _, err := svc.UpdateCategory(ctx, "admin-1", "c1", CategoryPatch{AttributesSchema: json.RawMessage(`{ "type" : "object" }`)}, ""); err != nil {
		t.Fatal(err)
	}
	if len(repo.audits) != n {
		t.Fatal("si nada cambia no se escribe ni se audita")
	}
	if _, err := svc.UpdateCategory(ctx, "admin-1", "no-existe", CategoryPatch{}, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("editar una categoría inexistente: %v", err)
	}
}

func TestAdminCityAndScope(t *testing.T) {
	svc, repo, _ := newCatalogAdminFixture()
	ctx := context.Background()

	city, err := svc.UpdateCity(ctx, "admin-1", "ayacucho", CityPatch{Enabled: ptr(false)}, "")
	if err != nil || city.Enabled || repo.cities["ayacucho"].Enabled {
		t.Fatalf("UpdateCity = %+v, %v", city, err)
	}
	n := len(repo.audits)
	if _, err := svc.UpdateCity(ctx, "admin-1", "ayacucho", CityPatch{Enabled: ptr(false)}, ""); err != nil || len(repo.audits) != n {
		t.Fatal("apagar una ciudad ya apagada no audita de nuevo")
	}

	if err := svc.SetCategoryCityScope(ctx, "admin-1", "c1", "ayacucho", ptr(false), ""); err != nil {
		t.Fatal(err)
	}
	if v := repo.scopes["c1|city-1"]; v == nil || *v {
		t.Fatal("la categoría queda apagada solo en Ayacucho")
	}
	if err := svc.SetCategoryCityScope(ctx, "admin-1", "c1", "cusco", ptr(true), ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("una ciudad que no existe: %v", err)
	}
}

func TestAdminSetSetting(t *testing.T) {
	svc, repo, _ := newCatalogAdminFixture()
	ctx := context.Background()

	s, err := svc.SetSetting(ctx, "admin-1", SettingInput{Key: "rental.owner_commission_bps", Value: json.RawMessage("900")}, "")
	if err != nil || string(s.Value) != "900" || s.Version != 2 {
		t.Fatalf("SetSetting = %+v, %v", s, err)
	}
	audit := repo.audits[len(repo.audits)-1]
	if audit.Action != domain.AuditSettingChanged || audit.After.(map[string]any)["key"] != "rental.owner_commission_bps" {
		t.Fatalf("el historial queda en la auditoría con la clave: %+v", audit)
	}

	// Por ciudad: alcance nuevo, sin valor anterior.
	s, err = svc.SetSetting(ctx, "admin-1", SettingInput{Key: "rental.owner_commission_bps", CitySlug: "ayacucho", Value: json.RawMessage("800")}, "")
	if err != nil || s.CityID != "city-1" || repo.audits[len(repo.audits)-1].Before != nil {
		t.Fatalf("ajuste por ciudad: %+v %v", s, err)
	}

	n := len(repo.audits)
	if _, err := svc.SetSetting(ctx, "admin-1", SettingInput{Key: "rental.owner_commission_bps", Value: json.RawMessage(" 900 ")}, ""); err != nil || len(repo.audits) != n {
		t.Fatal("el mismo valor no se vuelve a escribir")
	}
	for _, in := range []SettingInput{
		{Key: "rental.owner_commission_bps", Value: json.RawMessage("10.5")},
		{Key: "rental.comision", Value: json.RawMessage("100")},
		{Key: "rental.owner_commission_bps", CitySlug: "cusco", Value: json.RawMessage("100")},
	} {
		if _, err := svc.SetSetting(ctx, "admin-1", in, ""); err == nil {
			t.Fatalf("%+v debe rechazarse", in)
		}
	}
	if _, err := svc.SettingHistory(ctx, "rental.comision", 10); !errors.Is(err, domain.ErrUnknownSetting) {
		t.Fatalf("historial de una clave desconocida: %v", err)
	}
}
