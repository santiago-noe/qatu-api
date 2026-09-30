package service

import (
	"context"
	"errors"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// LenderInput es lo que la persona completa para activar (o editar) su perfil de arrendador.
type LenderInput struct {
	Kind         domain.LenderKind
	BusinessName string
	Phone        string
	CitySlug     string
	ZoneSlug     string
	// AcceptTerms: acepta las condiciones de arrendador; solo se exige la primera vez.
	AcceptTerms bool
}

// LenderView es el perfil con su ciudad y distrito ya resueltos para mostrarlos.
type LenderView struct {
	Profile  domain.LenderProfile
	Location domain.Location
}

// LenderService activa el rol de arrendador (decisión de clarify de la 003): correo verificado,
// celular privado, distrito y condiciones aceptadas.
type LenderService struct {
	accounts     port.AccountRepository
	lenders      port.LenderRepository
	catalog      *CatalogService
	clock        port.Clock
	termsVersion string
}

func NewLenderService(accounts port.AccountRepository, lenders port.LenderRepository, catalog *CatalogService,
	clock port.Clock, termsVersion string) *LenderService {
	return &LenderService{accounts: accounts, lenders: lenders, catalog: catalog, clock: clock, termsVersion: termsVersion}
}

// Get devuelve el perfil; ok = false si la persona aún no es arrendadora.
func (s *LenderService) Get(ctx context.Context, userID string) (LenderView, bool, error) {
	p, err := s.lenders.FindLenderProfile(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return LenderView{}, false, nil
	}
	if err != nil {
		return LenderView{}, false, err
	}
	loc, _, err := locationByIDs(ctx, s.catalog, p.CityID, p.ZoneID)
	return LenderView{Profile: p, Location: loc}, true, err
}

// Save activa el perfil (da el rol lender) o lo actualiza.
func (s *LenderService) Save(ctx context.Context, userID string, in LenderInput, ip string) (LenderView, error) {
	user, err := s.accounts.FindUser(ctx, userID)
	if err != nil {
		return LenderView{}, err
	}
	if !user.IsVerified() {
		return LenderView{}, domain.ErrLenderEmailUnverified
	}
	current, err := s.lenders.FindLenderProfile(ctx, userID)
	first := errors.Is(err, domain.ErrNotFound)
	if err != nil && !first {
		return LenderView{}, err
	}
	if first && !in.AcceptTerms {
		return LenderView{}, domain.ErrLenderTermsRequired
	}

	loc, err := s.resolveLocation(ctx, in.CitySlug, in.ZoneSlug)
	if err != nil {
		return LenderView{}, err
	}
	profile, err := domain.NormalizeLenderProfile(domain.LenderProfile{
		UserID: userID, Kind: in.Kind, BusinessName: in.BusinessName, Phone: in.Phone, CityID: loc.City.ID, ZoneID: loc.Zone.ID,
	})
	if err != nil {
		return LenderView{}, err
	}
	if !first && lenderUnchanged(current, profile) {
		return LenderView{Profile: current, Location: loc}, nil
	}

	activation := port.LenderActivation{Profile: profile, Audit: domain.AuditEntry{
		ActorID: userID, Action: domain.AuditLenderUpdated, Entity: "lender_profile", EntityID: userID,
		// El celular no va a la auditoría: basta saber que cambió.
		After: map[string]any{"kind": profile.Kind, "business_name": profile.BusinessName, "city_id": profile.CityID,
			"zone_id": profile.ZoneID, "phone_changed": first || current.Phone != profile.Phone},
		IP: ip,
	}}
	if first {
		activation.Audit.Action = domain.AuditLenderActivated
		activation.Consent = &domain.Consent{Purpose: domain.ConsentLenderTerms, Version: s.termsVersion, GrantedAt: s.clock.Now()}
	}
	if err := s.lenders.SaveLenderProfile(ctx, activation); err != nil {
		return LenderView{}, err
	}
	saved, err := s.lenders.FindLenderProfile(ctx, userID)
	return LenderView{Profile: saved, Location: loc}, err
}

// resolveLocation busca la ciudad y el distrito habilitados; cualquier otro caso es ubicación inválida.
func (s *LenderService) resolveLocation(ctx context.Context, citySlug, zoneSlug string) (domain.Location, error) {
	city, err := s.catalog.City(ctx, citySlug)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Location{}, domain.ErrInvalidLocation
	}
	if err != nil {
		return domain.Location{}, err
	}
	zones, err := s.catalog.Zones(ctx, citySlug)
	if err != nil {
		return domain.Location{}, err
	}
	for _, z := range zones {
		if z.Slug == zoneSlug {
			return domain.Location{City: city, Zone: z}, nil
		}
	}
	return domain.Location{}, domain.ErrInvalidLocation
}

func lenderUnchanged(a, b domain.LenderProfile) bool {
	return a.Kind == b.Kind && a.BusinessName == b.BusinessName && a.Phone == b.Phone && a.CityID == b.CityID && a.ZoneID == b.ZoneID
}

// locationByIDs resuelve una ciudad y un distrito guardados por ID con el catálogo en caché;
// ok = false si alguno ya no está habilitado.
func locationByIDs(ctx context.Context, catalog *CatalogService, cityID, zoneID string) (domain.Location, bool, error) {
	cities, err := catalog.Cities(ctx)
	if err != nil {
		return domain.Location{}, false, err
	}
	for _, city := range cities {
		if city.ID != cityID {
			continue
		}
		zones, err := catalog.Zones(ctx, city.Slug)
		if err != nil {
			return domain.Location{}, false, err
		}
		for _, z := range zones {
			if z.ID == zoneID {
				return domain.Location{City: city, Zone: z}, true, nil
			}
		}
		return domain.Location{City: city}, false, nil
	}
	return domain.Location{}, false, nil
}
