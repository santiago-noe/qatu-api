package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// fakeCatalog cuenta las lecturas para comprobar que la caché las ahorra.
type fakeCatalog struct {
	calls map[string]int
}

var ayacucho = domain.City{ID: "city-1", Slug: "ayacucho", Name: "Ayacucho", Enabled: true}

func (f *fakeCatalog) ListCategories(_ context.Context, v domain.Vertical, cityID string) ([]domain.Category, error) {
	f.calls["categories:"+string(v)+":"+cityID]++
	return []domain.Category{
		{ID: "c1", Vertical: v, Slug: "construccion"},
		{ID: "t1", Vertical: v, ParentID: "c1", Slug: "rotomartillo"},
	}, nil
}

func (f *fakeCatalog) ListCities(context.Context) ([]domain.City, error) {
	f.calls["cities"]++
	return []domain.City{ayacucho}, nil
}

func (f *fakeCatalog) ListZones(_ context.Context, cityID string) ([]domain.Zone, error) {
	f.calls["zones:"+cityID]++
	return []domain.Zone{{ID: "z1", CityID: cityID, Slug: "carmen-alto"}}, nil
}

func (f *fakeCatalog) LocationAt(_ context.Context, p domain.GeoPoint) (domain.Location, error) {
	if p.Lat > -13 {
		return domain.Location{}, domain.ErrOutOfCoverage
	}
	return domain.Location{City: ayacucho, Zone: domain.Zone{Slug: "ayacucho"}}, nil
}

// memoryCache tiene la semántica del adaptador de Redis; broken simula Redis caído.
type memoryCache struct {
	mu     sync.Mutex
	data   map[string][]byte
	broken bool
}

func newMemoryCache() *memoryCache { return &memoryCache{data: map[string][]byte{}} }

var errRedisDown = errors.New("redis caído")

func (m *memoryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.broken {
		return nil, false, errRedisDown
	}
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *memoryCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.broken {
		return errRedisDown
	}
	m.data[key] = value
	return nil
}

func (m *memoryCache) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			delete(m.data, k)
		}
	}
	return nil
}

func newCatalogFixture() (*CatalogService, *fakeCatalog, *memoryCache) {
	reader := &fakeCatalog{calls: map[string]int{}}
	cache := newMemoryCache()
	return NewCatalogService(reader, cache), reader, cache
}

func TestCatalogCategoriesTreeAndCache(t *testing.T) {
	svc, reader, cache := newCatalogFixture()
	ctx := context.Background()

	for range 3 {
		tree, err := svc.Categories(ctx, domain.VerticalRental, "ayacucho")
		if err != nil {
			t.Fatal(err)
		}
		if len(tree) != 1 || len(tree[0].Children) != 1 || tree[0].Children[0].Slug != "rotomartillo" {
			t.Fatalf("debe devolver el árbol: %+v", tree)
		}
	}
	if n := reader.calls["categories:rental:city-1"]; n != 1 {
		t.Fatalf("tres pedidos, una sola lectura a la base; hubo %d", n)
	}
	if reader.calls["cities"] != 1 {
		t.Fatal("la ciudad también sale de la caché")
	}

	_ = cache.DeletePrefix(ctx, CatalogCachePrefix)
	_, _ = svc.Categories(ctx, domain.VerticalRental, "ayacucho")
	if reader.calls["categories:rental:city-1"] != 2 {
		t.Fatal("al invalidar el catálogo se vuelve a leer de la base")
	}
}

func TestCatalogWorksWithRedisDown(t *testing.T) {
	svc, _, cache := newCatalogFixture()
	cache.broken = true
	zones, err := svc.Zones(context.Background(), "ayacucho")
	if err != nil || len(zones) != 1 || zones[0].Slug != "carmen-alto" {
		t.Fatalf("sin Redis el catálogo se lee de la base: %+v %v", zones, err)
	}
}

func TestCatalogUnknownCity(t *testing.T) {
	svc, _, _ := newCatalogFixture()
	ctx := context.Background()
	if _, err := svc.Zones(ctx, "cusco"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("una ciudad no habilitada no existe, llegó %v", err)
	}
	if _, err := svc.Categories(ctx, domain.VerticalService, "cusco"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ídem para las categorías, llegó %v", err)
	}
}

func TestCatalogLocationAt(t *testing.T) {
	svc, _, _ := newCatalogFixture()
	ctx := context.Background()
	loc, err := svc.LocationAt(ctx, domain.GeoPoint{Lat: -13.1631, Lng: -74.2237})
	if err != nil || loc.Zone.Slug != "ayacucho" {
		t.Fatalf("LocationAt = %+v, %v", loc, err)
	}
	if _, err := svc.LocationAt(ctx, domain.GeoPoint{Lat: -12.04, Lng: -77.04}); err != nil && !errors.Is(err, domain.ErrOutOfCoverage) {
		t.Fatalf("fuera de la ciudad: %v", err)
	}
	if _, err := svc.LocationAt(ctx, domain.GeoPoint{Lat: 120}); !errors.Is(err, domain.ErrInvalidLocation) {
		t.Fatalf("una coordenada imposible se rechaza antes de consultar, llegó %v", err)
	}
}
