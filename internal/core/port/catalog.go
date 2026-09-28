package port

import (
	"context"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// CatalogReader lee el catálogo público: solo lo habilitado y publicable. Implementación: Postgres.
type CatalogReader interface {
	// ListCategories devuelve la lista plana ordenada (raíces y tipos) de la vertical, sin
	// apagadas ni prohibidas. cityID vacío = sin ajustes por ciudad.
	ListCategories(ctx context.Context, vertical domain.Vertical, cityID string) ([]domain.Category, error)
	// ListCities devuelve las ciudades habilitadas (son pocas: el servicio busca por slug en ella).
	ListCities(ctx context.Context) ([]domain.City, error)
	ListZones(ctx context.Context, cityID string) ([]domain.Zone, error)
	// LocationAt devuelve la ciudad y la zona que contienen el punto, o domain.ErrOutOfCoverage.
	LocationAt(ctx context.Context, p domain.GeoPoint) (domain.Location, error)
}
