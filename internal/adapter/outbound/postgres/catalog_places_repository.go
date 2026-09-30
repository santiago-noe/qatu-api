package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Implementa la parte de port.CatalogAdmin de distritos y del alcance de categorías por ciudad.

// boundaryFromGeoJSON convierte el GeoJSON ya validado por el dominio en el MultiPolygon de la
// columna. ST_MakeValid corrige autointersecciones de los límites exportados, como en 0004.
const boundaryFromGeoJSON = `ST_Multi(ST_CollectionExtract(ST_MakeValid(ST_GeomFromGeoJSON(%s::text)), 3))`

// boundaryArg: SQL NULL si no hay límite.
func boundaryArg(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (r *CatalogRepository) CategoryCityScopes(ctx context.Context, categoryID string) ([]domain.CategoryCityScope, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT `+cityColumns+`, o.enabled
		FROM cities c
		LEFT JOIN category_city_overrides o ON o.city_id = c.id AND o.category_id = $1
		ORDER BY c.name`, categoryID)
	if err != nil {
		return nil, mapCatalogError(err)
	}
	scopes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CategoryCityScope, error) {
		var s domain.CategoryCityScope
		err := row.Scan(append(cityDest(&s.City), &s.Override)...)
		return s, err
	})
	return scopes, mapCatalogError(err)
}

// ListAllZones incluye el límite simplificado (unos 10 m) en GeoJSON: el admin lo dibuja sin
// bajar miles de vértices. ST_Multi mantiene el tipo: simplificar un solo polígono da Polygon.
func (r *CatalogRepository) ListAllZones(ctx context.Context, cityID string) ([]domain.Zone, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT `+zoneColumns+`, COALESCE(ST_AsGeoJSON(ST_Multi(ST_SimplifyPreserveTopology(z.boundary, 0.0001)), 6), '')
		FROM zones z WHERE z.city_id = $1 ORDER BY z.sort_order, z.name`, cityID)
	if err != nil {
		return nil, mapCatalogError(err)
	}
	zones, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Zone, error) {
		var z domain.Zone
		var boundary string
		err := row.Scan(append(zoneDest(&z), &boundary)...)
		if boundary != "" {
			z.Boundary = json.RawMessage(boundary)
		}
		return z, err
	})
	return zones, mapCatalogError(err)
}

func (r *CatalogRepository) FindZone(ctx context.Context, cityID, slug string) (domain.Zone, error) {
	z, err := scanZone(r.db.Pool.QueryRow(ctx, zoneSelect+` WHERE z.city_id = $1 AND z.slug = $2`, cityID, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Zone{}, domain.ErrNotFound
	}
	return z, mapCatalogError(err)
}

func (r *CatalogRepository) CreateZone(ctx context.Context, z domain.Zone, audit domain.AuditEntry) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO zones (city_id, slug, name, ubigeo, sort_order, enabled, boundary)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, `+fmt.Sprintf(boundaryFromGeoJSON, "$7")+`)
			RETURNING id`, z.CityID, z.Slug, z.Name, z.Ubigeo, z.SortOrder, z.Enabled, boundaryArg(z.Boundary)).Scan(&id); err != nil {
			return err
		}
		audit.EntityID = id
		return insertAudit(ctx, tx, audit)
	})
	return id, mapCatalogError(err)
}

func (r *CatalogRepository) UpdateZone(ctx context.Context, z domain.Zone, audit domain.AuditEntry) error {
	err := pgx.BeginFunc(ctx, r.db.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE zones SET name = $2, sort_order = $3, enabled = $4,
			       boundary = CASE WHEN $5::text IS NULL THEN boundary ELSE `+fmt.Sprintf(boundaryFromGeoJSON, "$5")+` END
			WHERE id = $1`, z.ID, z.Name, z.SortOrder, z.Enabled, boundaryArg(z.Boundary))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, audit)
	})
	return mapCatalogError(err)
}

func (r *CatalogRepository) ZoneOverlap(ctx context.Context, cityID, exceptZoneID string, boundary json.RawMessage) (float64, error) {
	var ratio float64
	err := r.db.Pool.QueryRow(ctx, `
		WITH g AS (SELECT `+fmt.Sprintf(boundaryFromGeoJSON, "$3")+` AS geom)
		SELECT COALESCE(MAX(ST_Area(ST_Intersection(z.boundary, g.geom)::geography)
		                    / NULLIF(ST_Area(g.geom::geography), 0)), 0)
		FROM zones z, g
		WHERE z.city_id = $1 AND z.id IS DISTINCT FROM NULLIF($2, '')::uuid
		  AND z.boundary IS NOT NULL AND ST_Intersects(z.boundary, g.geom)`,
		cityID, exceptZoneID, string(boundary)).Scan(&ratio)
	return ratio, mapCatalogError(err)
}
