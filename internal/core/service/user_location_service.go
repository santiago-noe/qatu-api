package service

import (
	"context"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// UserLocationService guarda la ciudad y el distrito del usuario (spec 002: "que la app detecte o
// me deje elegir mi ciudad y zona"). Valida contra el catálogo público (en caché): solo ciudades y
// distritos habilitados.
type UserLocationService struct {
	accounts port.AccountRepository
	catalog  *CatalogService
}

func NewUserLocationService(accounts port.AccountRepository, catalog *CatalogService) *UserLocationService {
	return &UserLocationService{accounts: accounts, catalog: catalog}
}

// Get devuelve la ubicación guardada; ok = false si aún no eligió o si su distrito se deshabilitó
// (la app le vuelve a pedir que elija).
func (s *UserLocationService) Get(ctx context.Context, userID string) (loc domain.Location, ok bool, err error) {
	user, err := s.accounts.FindUser(ctx, userID)
	if err != nil || user.CityID == "" {
		return domain.Location{}, false, err
	}
	cities, err := s.catalog.Cities(ctx)
	if err != nil {
		return domain.Location{}, false, err
	}
	for _, city := range cities {
		if city.ID != user.CityID {
			continue
		}
		zone, found, err := s.findZone(ctx, city.Slug, func(z domain.Zone) bool { return z.ID == user.ZoneID })
		if err != nil || !found {
			return domain.Location{}, false, err
		}
		return domain.Location{City: city, Zone: zone}, true, nil
	}
	return domain.Location{}, false, nil
}

// Set guarda la ciudad y el distrito elegidos (de la lista o detectados con /geo/zone).
func (s *UserLocationService) Set(ctx context.Context, userID, citySlug, zoneSlug, ip string) (domain.Location, error) {
	city, err := s.catalog.City(ctx, citySlug)
	if err != nil {
		return domain.Location{}, err
	}
	zone, found, err := s.findZone(ctx, citySlug, func(z domain.Zone) bool { return z.Slug == zoneSlug })
	if err != nil {
		return domain.Location{}, err
	}
	if !found {
		return domain.Location{}, domain.ErrNotFound
	}
	loc := domain.Location{City: city, Zone: zone}

	user, err := s.accounts.FindUser(ctx, userID)
	if err != nil {
		return domain.Location{}, err
	}
	if user.CityID == city.ID && user.ZoneID == zone.ID {
		return loc, nil
	}
	err = s.accounts.UpdateLocation(ctx, userID, city.ID, zone.ID, domain.AuditEntry{
		ActorID: userID, Action: domain.AuditLocationUpdated, Entity: "user", EntityID: userID,
		Before: map[string]any{"city_id": user.CityID, "zone_id": user.ZoneID},
		After:  map[string]any{"city_id": city.ID, "zone_id": zone.ID}, IP: ip,
	})
	return loc, err
}

func (s *UserLocationService) findZone(ctx context.Context, citySlug string, match func(domain.Zone) bool) (domain.Zone, bool, error) {
	zones, err := s.catalog.Zones(ctx, citySlug)
	if err != nil {
		return domain.Zone{}, false, err
	}
	for _, z := range zones {
		if match(z) {
			return z, true, nil
		}
	}
	return domain.Zone{}, false, nil
}
