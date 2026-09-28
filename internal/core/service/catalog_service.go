package service

import (
	"context"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// CatalogCachePrefix agrupa las claves del catálogo: el admin las invalida todas al escribir.
const CatalogCachePrefix = "catalog:"

// catalogTTL: el catálogo cambia poco; igual se invalida al editarlo (docs/04: 60 s a 1 h).
const catalogTTL = time.Hour

// CatalogService expone el catálogo público: categorías, oficios, ciudades y zonas.
type CatalogService struct {
	reader port.CatalogReader
	cache  port.Cache
}

func NewCatalogService(reader port.CatalogReader, cache port.Cache) *CatalogService {
	return &CatalogService{reader: reader, cache: cache}
}

// Categories devuelve el árbol de la vertical. Con citySlug aplica lo activado en esa ciudad.
func (s *CatalogService) Categories(ctx context.Context, vertical domain.Vertical, citySlug string) ([]domain.Category, error) {
	cityID := ""
	if citySlug != "" {
		city, err := s.City(ctx, citySlug)
		if err != nil {
			return nil, err
		}
		cityID = city.ID
	}
	key := CatalogCachePrefix + "categories:" + string(vertical) + ":" + citySlug
	return cached(ctx, s.cache, key, catalogTTL, func() ([]domain.Category, error) {
		flat, err := s.reader.ListCategories(ctx, vertical, cityID)
		return domain.BuildCategoryTree(flat), err
	})
}

// Cities devuelve las ciudades habilitadas.
func (s *CatalogService) Cities(ctx context.Context) ([]domain.City, error) {
	return cached(ctx, s.cache, CatalogCachePrefix+"cities", catalogTTL, func() ([]domain.City, error) {
		return s.reader.ListCities(ctx)
	})
}

// City busca una ciudad habilitada en la lista en caché (son pocas), o domain.ErrNotFound.
func (s *CatalogService) City(ctx context.Context, slug string) (domain.City, error) {
	cities, err := s.Cities(ctx)
	if err != nil {
		return domain.City{}, err
	}
	for _, c := range cities {
		if c.Slug == slug {
			return c, nil
		}
	}
	return domain.City{}, domain.ErrNotFound
}

// Zones devuelve los distritos habilitados de la ciudad.
func (s *CatalogService) Zones(ctx context.Context, citySlug string) ([]domain.Zone, error) {
	city, err := s.City(ctx, citySlug)
	if err != nil {
		return nil, err
	}
	return cached(ctx, s.cache, CatalogCachePrefix+"zones:"+citySlug, catalogTTL, func() ([]domain.Zone, error) {
		return s.reader.ListZones(ctx, city.ID)
	})
}

// LocationAt detecta la ciudad y el distrito de un punto (ubicación del navegador). Sin caché:
// cada punto es distinto y la consulta usa el índice GIST.
func (s *CatalogService) LocationAt(ctx context.Context, p domain.GeoPoint) (domain.Location, error) {
	if err := p.Validate(); err != nil {
		return domain.Location{}, err
	}
	return s.reader.LocationAt(ctx, p)
}
