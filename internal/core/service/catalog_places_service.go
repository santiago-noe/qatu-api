package service

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// Ciudades y distritos desde el admin (spec 002): una ciudad nueva nace apagada, se le cargan sus
// distritos con el límite oficial y recién entonces se enciende.

// CityPatch son los cambios de una ciudad; nil = no cambia. El slug y el ubigeo no se editan
// (van en direcciones y en los ubigeos de sus distritos).
type CityPatch struct {
	Name    *string
	Region  *string
	Center  *domain.GeoPoint
	Enabled *bool
}

// ZonePatch son los cambios de un distrito; Boundary nil = conserva su límite.
type ZonePatch struct {
	Name      *string
	SortOrder *int
	Enabled   *bool
	Boundary  json.RawMessage
}

// Cities devuelve todas las ciudades, también las apagadas (el admin las enciende).
func (s *CatalogAdminService) Cities(ctx context.Context) ([]domain.City, error) {
	return s.repo.ListAllCities(ctx)
}

// CreateCity da de alta una ciudad apagada.
func (s *CatalogAdminService) CreateCity(ctx context.Context, actorID string, in domain.City, ip string) (domain.City, error) {
	city, err := domain.NormalizeCity(in)
	if err != nil {
		return domain.City{}, err
	}
	city.Enabled = false
	_, err = s.repo.CreateCity(ctx, city, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditCityCreated, Entity: "city", After: citySnapshot(city), IP: ip,
	})
	if err != nil {
		return domain.City{}, err
	}
	return s.repo.FindCityBySlug(ctx, city.Slug)
}

// UpdateCity cambia los datos de una ciudad o la enciende y apaga (feature flag). Solo se enciende
// con al menos un distrito activo.
func (s *CatalogAdminService) UpdateCity(ctx context.Context, actorID, citySlug string, p CityPatch, ip string) (domain.City, error) {
	current, err := s.repo.FindCityBySlug(ctx, citySlug)
	if err != nil {
		return domain.City{}, err
	}
	next := current
	if p.Name != nil {
		next.Name = *p.Name
	}
	if p.Region != nil {
		next.Region = *p.Region
	}
	if p.Center != nil {
		next.Center = *p.Center
	}
	if p.Enabled != nil {
		next.Enabled = *p.Enabled
	}
	if next, err = domain.NormalizeCity(next); err != nil {
		return domain.City{}, err
	}
	before, after := citySnapshot(current), citySnapshot(next)
	if reflect.DeepEqual(before, after) {
		return current, nil
	}
	if next.Enabled && !current.Enabled {
		zones, err := s.repo.ListAllZones(ctx, current.ID)
		if err != nil {
			return domain.City{}, err
		}
		if err := domain.CheckCityCanEnable(zones); err != nil {
			return domain.City{}, err
		}
	}
	err = s.repo.UpdateCity(ctx, next, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditCityUpdated, Entity: "city", EntityID: current.ID,
		Before: before, After: after, IP: ip,
	})
	if err != nil {
		return domain.City{}, err
	}
	s.invalidate(ctx)
	return next, nil
}

// CategoryCities devuelve la categoría y cómo está en cada ciudad (con ajuste propio o sin él).
func (s *CatalogAdminService) CategoryCities(ctx context.Context, categoryID string) (domain.Category, []domain.CategoryCityScope, error) {
	category, err := s.repo.FindCategory(ctx, categoryID)
	if err != nil {
		return domain.Category{}, nil, err
	}
	scopes, err := s.repo.CategoryCityScopes(ctx, categoryID)
	return category, scopes, err
}

// Zones devuelve la ciudad y todos sus distritos, con el límite simplificado para dibujarlos.
func (s *CatalogAdminService) Zones(ctx context.Context, citySlug string) (domain.City, []domain.Zone, error) {
	city, err := s.repo.FindCityBySlug(ctx, citySlug)
	if err != nil {
		return domain.City{}, nil, err
	}
	zones, err := s.repo.ListAllZones(ctx, city.ID)
	return city, zones, err
}

// CreateZone agrega un distrito a la ciudad. El límite es opcional: sin él, el distrito se elige
// de la lista pero no se detecta por ubicación.
func (s *CatalogAdminService) CreateZone(ctx context.Context, actorID, citySlug string, in domain.Zone, ip string) (domain.Zone, error) {
	city, err := s.repo.FindCityBySlug(ctx, citySlug)
	if err != nil {
		return domain.Zone{}, err
	}
	zone, err := domain.NormalizeZone(in, city)
	if err != nil {
		return domain.Zone{}, err
	}
	zone.CityID = city.ID
	if zone.Boundary != nil {
		if zone.Boundary, err = s.checkBoundary(ctx, city, "", zone.Boundary); err != nil {
			return domain.Zone{}, err
		}
	}
	if _, err := s.repo.CreateZone(ctx, zone, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditZoneCreated, Entity: "zone", After: zoneSnapshot(zone, city, zone.Boundary != nil), IP: ip,
	}); err != nil {
		return domain.Zone{}, err
	}
	s.invalidate(ctx)
	return s.repo.FindZone(ctx, city.ID, zone.Slug)
}

// UpdateZone cambia nombre, orden, estado o límite. No deja una ciudad encendida sin distritos activos.
func (s *CatalogAdminService) UpdateZone(ctx context.Context, actorID, citySlug, zoneSlug string, p ZonePatch, ip string) (domain.Zone, error) {
	city, err := s.repo.FindCityBySlug(ctx, citySlug)
	if err != nil {
		return domain.Zone{}, err
	}
	current, err := s.repo.FindZone(ctx, city.ID, zoneSlug)
	if err != nil {
		return domain.Zone{}, err
	}
	next := current
	if p.Name != nil {
		next.Name = *p.Name
	}
	if p.SortOrder != nil {
		next.SortOrder = *p.SortOrder
	}
	if p.Enabled != nil {
		next.Enabled = *p.Enabled
	}
	if next, err = domain.NormalizeZone(next, city); err != nil {
		return domain.Zone{}, err
	}
	if p.Boundary != nil {
		if next.Boundary, err = s.checkBoundary(ctx, city, current.ID, p.Boundary); err != nil {
			return domain.Zone{}, err
		}
		next.HasBoundary = true
	}
	before, after := zoneSnapshot(current, city, false), zoneSnapshot(next, city, next.Boundary != nil)
	if reflect.DeepEqual(before, after) {
		return current, nil
	}
	if city.Enabled && current.Enabled && !next.Enabled {
		if err := s.checkNotLastZone(ctx, city, current.ID); err != nil {
			return domain.Zone{}, err
		}
	}
	if err := s.repo.UpdateZone(ctx, next, domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditZoneUpdated, Entity: "zone", EntityID: current.ID,
		Before: before, After: after, IP: ip,
	}); err != nil {
		return domain.Zone{}, err
	}
	s.invalidate(ctx)
	next.Boundary = nil // la respuesta no devuelve el límite completo
	return next, nil
}

// checkBoundary valida el GeoJSON, que caiga en la ciudad y que no repita otro distrito.
func (s *CatalogAdminService) checkBoundary(ctx context.Context, city domain.City, exceptZoneID string, raw json.RawMessage) (json.RawMessage, error) {
	b, err := domain.ParseBoundary(raw)
	if err != nil {
		return nil, err
	}
	if err := domain.CheckZoneNearCity(b, city); err != nil {
		return nil, err
	}
	overlap, err := s.repo.ZoneOverlap(ctx, city.ID, exceptZoneID, b.GeoJSON)
	if err != nil {
		return nil, err
	}
	if overlap > domain.MaxZoneOverlap {
		return nil, domain.ErrZoneOverlap
	}
	return b.GeoJSON, nil
}

// checkNotLastZone: apagar el último distrito activo de una ciudad encendida la dejaría sin zonas.
func (s *CatalogAdminService) checkNotLastZone(ctx context.Context, city domain.City, zoneID string) error {
	zones, err := s.repo.ListAllZones(ctx, city.ID)
	if err != nil {
		return err
	}
	others := make([]domain.Zone, 0, len(zones))
	for _, z := range zones {
		if z.ID != zoneID {
			others = append(others, z)
		}
	}
	return domain.CheckCityCanEnable(others)
}

func citySnapshot(c domain.City) map[string]any {
	return map[string]any{
		"slug": c.Slug, "name": c.Name, "region": c.Region, "ubigeo": c.Ubigeo,
		"center": []float64{c.Center.Lng, c.Center.Lat}, "enabled": c.Enabled,
	}
}

// zoneSnapshot no guarda el polígono (miles de vértices) en la auditoría: solo si cambió.
func zoneSnapshot(z domain.Zone, city domain.City, boundaryChanged bool) map[string]any {
	return map[string]any{
		"city": city.Slug, "slug": z.Slug, "name": z.Name, "ubigeo": z.Ubigeo, "sort_order": z.SortOrder,
		"enabled": z.Enabled, "boundary_changed": boundaryChanged,
	}
}
