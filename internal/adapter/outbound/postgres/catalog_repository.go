package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// CatalogRepository implementa port.CatalogReader.
type CatalogRepository struct {
	db *Client
}

func NewCatalogRepository(db *Client) *CatalogRepository { return &CatalogRepository{db: db} }

// ListCategories: lo publicable de la vertical. Un ajuste por ciudad (category_city_overrides)
// manda sobre el `enabled` global; las prohibidas nunca aparecen.
func (r *CatalogRepository) ListCategories(ctx context.Context, vertical domain.Vertical, cityID string) ([]domain.Category, error) {
	return r.queryCategories(ctx, categorySelect+`
		LEFT JOIN category_city_overrides o ON o.category_id = c.id AND o.city_id = NULLIF($2, '')::uuid
		WHERE c.vertical = $1 AND NOT c.prohibited AND COALESCE(o.enabled, c.enabled)`+categoryOrder, vertical, cityID)
}

// categorySelect y scanCategory son la única lectura de categorías (pública y del admin).
const categorySelect = `
	SELECT c.id, c.vertical, COALESCE(c.parent_id::text, ''), c.slug, c.name, COALESCE(c.description, ''),
	       COALESCE(c.icon, ''), c.sort_order, c.attributes_schema, c.risk_level, c.prohibited, c.enabled
	FROM categories c`

// Raíces primero y luego sus tipos, cada grupo en su orden: BuildCategoryTree lo necesita así.
const categoryOrder = `
	ORDER BY c.parent_id NULLS FIRST, c.sort_order, c.name`

func scanCategory(row pgx.Row) (domain.Category, error) {
	var c domain.Category
	err := row.Scan(&c.ID, &c.Vertical, &c.ParentID, &c.Slug, &c.Name, &c.Description, &c.Icon,
		&c.SortOrder, &c.AttributesSchema, &c.RiskLevel, &c.Prohibited, &c.Enabled)
	return c, err
}

func (r *CatalogRepository) queryCategories(ctx context.Context, sql string, args ...any) ([]domain.Category, error) {
	rows, err := r.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Category, error) { return scanCategory(row) })
}

const citySelect = `
	SELECT id, slug, name, region, timezone, ST_Y(center), ST_X(center), enabled FROM cities`

func scanCity(row pgx.Row) (domain.City, error) {
	var c domain.City
	err := row.Scan(&c.ID, &c.Slug, &c.Name, &c.Region, &c.Timezone, &c.Center.Lat, &c.Center.Lng, &c.Enabled)
	return c, err
}

func (r *CatalogRepository) ListCities(ctx context.Context) ([]domain.City, error) {
	rows, err := r.db.Pool.Query(ctx, citySelect+` WHERE enabled ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.City, error) { return scanCity(row) })
}

const zoneSelect = `
	SELECT z.id, z.city_id, z.slug, z.name, COALESCE(z.ubigeo, ''), z.sort_order, z.boundary IS NOT NULL FROM zones z`

func scanZone(row pgx.Row) (domain.Zone, error) {
	var z domain.Zone
	err := row.Scan(&z.ID, &z.CityID, &z.Slug, &z.Name, &z.Ubigeo, &z.SortOrder, &z.HasBoundary)
	return z, err
}

func (r *CatalogRepository) ListZones(ctx context.Context, cityID string) ([]domain.Zone, error) {
	rows, err := r.db.Pool.Query(ctx, zoneSelect+` WHERE z.city_id = $1 AND z.enabled ORDER BY z.sort_order, z.name`, cityID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Zone, error) { return scanZone(row) })
}

// LocationAt usa zone_at (migración 0003): índice GIST y bordes incluidos.
func (r *CatalogRepository) LocationAt(ctx context.Context, p domain.GeoPoint) (domain.Location, error) {
	zone, err := scanZone(r.db.Pool.QueryRow(ctx, zoneSelect+` WHERE z.id = zone_at($1, $2)`, p.Lng, p.Lat))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Location{}, domain.ErrOutOfCoverage
	}
	if err != nil {
		return domain.Location{}, err
	}
	city, err := scanCity(r.db.Pool.QueryRow(ctx, citySelect+` WHERE id = $1`, zone.CityID))
	if err != nil {
		return domain.Location{}, err
	}
	return domain.Location{City: city, Zone: zone}, nil
}
