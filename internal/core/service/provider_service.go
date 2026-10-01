package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// ProviderInput es todo lo que el proveedor edita de su perfil (el editor manda el formulario
// completo en cada guardado). Los oficios van por ID de categoría y los distritos por ID de zona.
// Un paquete sin ID es nuevo; uno con el ID de un paquete suyo lo reemplaza.
type ProviderInput struct {
	BusinessName    string
	Phone           string
	CitySlug        string
	Bio             string
	YearsExperience int
	WarrantyDays    *int // nil al crear: la garantía sugerida (domain.DefaultWarrantyDays)
	AcceptsUrgent   bool
	Trades          []domain.ProviderTrade
	CoverageZoneIDs []string
	Weekly          []domain.WeeklySlot
	// AcceptTerms: acepta las condiciones de proveedor; solo se pide al activar el perfil.
	AcceptTerms bool
}

// ProviderView es el perfil con su ciudad resuelta para mostrarla.
type ProviderView struct {
	Profile domain.ProviderProfile
	City    domain.City
}

// ProviderDeps agrupa lo que necesita ProviderService.
type ProviderDeps struct {
	Accounts     port.AccountRepository
	Providers    port.ProviderRepository
	Catalog      *CatalogService
	Clock        port.Clock
	IDs          port.IDGenerator
	TermsVersion string
}

// ProviderService: la persona activa su perfil de proveedor, lo completa, lo envía a revisión, lo
// pausa y bloquea días (spec 004). El rol provider llega con la aprobación de moderación (nivel P).
type ProviderService struct {
	d ProviderDeps
}

func NewProviderService(d ProviderDeps) *ProviderService { return &ProviderService{d: d} }

// Get devuelve el perfil; ok = false si la persona aún no es proveedora.
func (s *ProviderService) Get(ctx context.Context, userID string) (ProviderView, bool, error) {
	p, err := s.d.Providers.FindProvider(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return ProviderView{}, false, nil
	}
	if err != nil {
		return ProviderView{}, false, err
	}
	city, err := s.cityByID(ctx, p.CityID)
	return ProviderView{Profile: p, City: city}, true, err
}

func (s *ProviderService) profile(ctx context.Context, userID string) (domain.ProviderProfile, error) {
	p, err := s.d.Providers.FindProvider(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ProviderProfile{}, domain.ErrProviderProfileRequired
	}
	return p, err
}

// Activate crea el perfil en borrador: correo verificado y condiciones de proveedor aceptadas.
func (s *ProviderService) Activate(ctx context.Context, userID string, in ProviderInput, ip string) (ProviderView, error) {
	user, err := s.d.Accounts.FindUser(ctx, userID)
	if err != nil {
		return ProviderView{}, err
	}
	if !user.IsVerified() {
		return ProviderView{}, domain.ErrProviderEmailUnverified
	}
	if _, err := s.d.Providers.FindProvider(ctx, userID); err == nil {
		return ProviderView{}, domain.ErrProviderExists
	} else if !errors.Is(err, domain.ErrNotFound) {
		return ProviderView{}, err
	}
	if !in.AcceptTerms {
		return ProviderView{}, domain.ErrProviderTermsRequired
	}
	p, city, err := s.build(ctx, domain.ProviderProfile{
		UserID: userID, Status: domain.ListingDraft, WarrantyDays: domain.DefaultWarrantyDays,
	}, in)
	if err != nil {
		return ProviderView{}, err
	}
	consent := domain.Consent{Purpose: domain.ConsentProviderTerms, Version: s.d.TermsVersion, GrantedAt: s.d.Clock.Now()}
	if err := s.d.Providers.CreateProvider(ctx, p, consent, providerAudit(userID, domain.AuditProviderActivated, p, nil, ip)); err != nil {
		return ProviderView{}, err
	}
	saved, err := s.d.Providers.FindProvider(ctx, userID)
	return ProviderView{Profile: saved, City: city}, err
}

// Update reemplaza los datos. En revisión no se edita; publicado o pausado debe seguir completo
// (lo que ve el cliente nunca queda a medias). Cambiar precios no toca trabajos ya pedidos: cada
// uno guarda su copia (008, 009).
func (s *ProviderService) Update(ctx context.Context, userID string, version int, in ProviderInput, ip string) (ProviderView, error) {
	current, err := s.profile(ctx, userID)
	if err != nil {
		return ProviderView{}, err
	}
	if !current.Status.Editable() {
		return ProviderView{}, domain.ErrProviderNotEditable
	}
	next, city, err := s.build(ctx, current, in)
	if err != nil {
		return ProviderView{}, err
	}
	if current.Status == domain.ListingPublished || current.Status == domain.ListingPaused {
		if err := domain.CheckProviderComplete(next); err != nil {
			return ProviderView{}, err
		}
	}
	saved, err := s.d.Providers.UpdateProvider(ctx, next, version,
		providerAudit(userID, domain.AuditProviderUpdated, next, &current, ip))
	return ProviderView{Profile: saved, City: city}, err
}

// build aplica el formulario sobre el perfil y revisa contra el catálogo de la ciudad elegida.
func (s *ProviderService) build(ctx context.Context, p domain.ProviderProfile, in ProviderInput) (domain.ProviderProfile, domain.City, error) {
	city, err := s.d.Catalog.City(ctx, in.CitySlug)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ProviderProfile{}, domain.City{}, domain.ErrInvalidLocation
	}
	if err != nil {
		return domain.ProviderProfile{}, domain.City{}, err
	}
	known := p.PackageIDs()
	p.CityID, p.BusinessName, p.Phone, p.Bio = city.ID, in.BusinessName, in.Phone, in.Bio
	p.YearsExperience, p.AcceptsUrgent = in.YearsExperience, in.AcceptsUrgent
	if in.WarrantyDays != nil {
		p.WarrantyDays = *in.WarrantyDays
	}
	p.CoverageZoneIDs, p.Weekly = slices.Clone(in.CoverageZoneIDs), slices.Clone(in.Weekly)
	p.Trades = make([]domain.ProviderTrade, len(in.Trades))
	for i, t := range in.Trades {
		t.Packages = slices.Clone(t.Packages)
		for j := range t.Packages {
			if !slices.Contains(known, t.Packages[j].ID) {
				t.Packages[j].ID = s.d.IDs.NewID()
			}
		}
		p.Trades[i] = t
	}
	if p, err = domain.NormalizeProvider(p); err != nil {
		return domain.ProviderProfile{}, domain.City{}, err
	}
	return p, city, s.checkCatalog(ctx, city, p)
}

// checkCatalog: cada oficio está activo en la ciudad (el árbol público ya deja fuera lo apagado y
// lo prohibido) y cada distrito es de ella. Un oficio que el admin apagó o prohibió después
// obliga a quitarlo antes de volver a guardar o publicar.
func (s *ProviderService) checkCatalog(ctx context.Context, city domain.City, p domain.ProviderProfile) error {
	trades, err := s.d.Catalog.Categories(ctx, domain.VerticalService, city.Slug)
	if err != nil {
		return err
	}
	for _, t := range p.Trades {
		if !slices.ContainsFunc(trades, func(c domain.Category) bool { return c.ID == t.CategoryID }) {
			return domain.ErrProviderTrade
		}
	}
	zones, err := s.d.Catalog.Zones(ctx, city.Slug)
	if err != nil {
		return err
	}
	for _, id := range p.CoverageZoneIDs {
		if !slices.ContainsFunc(zones, func(z domain.Zone) bool { return z.ID == id }) {
			return domain.ErrProviderCoverage
		}
	}
	return nil
}

// cityByID resuelve la ciudad del perfil con el catálogo en caché; una ciudad apagada después ya
// no sirve para publicar.
func (s *ProviderService) cityByID(ctx context.Context, id string) (domain.City, error) {
	cities, err := s.d.Catalog.Cities(ctx)
	if err != nil {
		return domain.City{}, err
	}
	for _, c := range cities {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.City{}, domain.ErrInvalidLocation
}

// checkLive son las reglas para mostrarse: ciudad activa, oficios y distritos vigentes y datos completos.
func (s *ProviderService) checkLive(ctx context.Context, p domain.ProviderProfile) error {
	city, err := s.cityByID(ctx, p.CityID)
	if err != nil {
		return err
	}
	if err := s.checkCatalog(ctx, city, p); err != nil {
		return err
	}
	return domain.CheckProviderComplete(p)
}

// Submit envía el perfil: hasta la primera aprobación (nivel P) pasa por moderación.
func (s *ProviderService) Submit(ctx context.Context, userID string, version int, ip string) (ProviderView, error) {
	return s.liveTransition(ctx, userID, version, domain.ListingSubmit, ip)
}

// Pause lo oculta de las búsquedas sin perder nada; Resume lo vuelve a mostrar si sigue vigente.
func (s *ProviderService) Pause(ctx context.Context, userID string, version int, ip string) (ProviderView, error) {
	current, err := s.profile(ctx, userID)
	if err != nil {
		return ProviderView{}, err
	}
	return s.transition(ctx, userID, current, version, domain.ListingPause, ip)
}

func (s *ProviderService) Resume(ctx context.Context, userID string, version int, ip string) (ProviderView, error) {
	return s.liveTransition(ctx, userID, version, domain.ListingResume, ip)
}

func (s *ProviderService) liveTransition(ctx context.Context, userID string, version int, action domain.ListingAction, ip string) (ProviderView, error) {
	current, err := s.profile(ctx, userID)
	if err != nil {
		return ProviderView{}, err
	}
	if err := s.checkLive(ctx, current); err != nil {
		return ProviderView{}, err
	}
	return s.transition(ctx, userID, current, version, action, ip)
}

func (s *ProviderService) transition(ctx context.Context, actorID string, current domain.ProviderProfile, version int,
	action domain.ListingAction, ip string) (ProviderView, error) {
	next, err := domain.ApplyProviderAction(current, action, s.d.Clock.Now())
	if err != nil {
		return ProviderView{}, err
	}
	audit := domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditProviderStatus, Entity: "provider_profile", EntityID: current.UserID,
		Before: map[string]any{"status": current.Status}, After: map[string]any{"status": next.Status, "action": action}, IP: ip,
	}
	saved, err := s.d.Providers.UpdateProvider(ctx, next, version, audit)
	if err != nil {
		return ProviderView{}, err
	}
	city, err := s.cityByID(ctx, saved.CityID)
	return ProviderView{Profile: saved, City: city}, err
}

// Calendar devuelve los días bloqueados en la ventana pedida.
func (s *ProviderService) Calendar(ctx context.Context, userID string, from, to time.Time) ([]domain.AvailabilityBlock, error) {
	if _, err := s.profile(ctx, userID); err != nil {
		return nil, err
	}
	from, to, err := domain.CalendarWindow(from, to)
	if err != nil {
		return nil, err
	}
	return s.d.Providers.ListProviderBlocks(ctx, userID, from, to)
}

// BlockDates bloquea días a mano (vacaciones, otro trabajo). No se cruza con otro bloqueo ni trabajo.
func (s *ProviderService) BlockDates(ctx context.Context, userID string, b domain.AvailabilityBlock, ip string) (domain.AvailabilityBlock, error) {
	if _, err := s.profile(ctx, userID); err != nil {
		return domain.AvailabilityBlock{}, err
	}
	b, err := domain.NormalizeManualBlock(b, s.d.Clock.Now())
	if err != nil {
		return domain.AvailabilityBlock{}, err
	}
	b.CreatedBy = userID
	b.ID, err = s.d.Providers.AddProviderBlock(ctx, userID, b, domain.AuditEntry{
		ActorID: userID, Action: domain.AuditProviderBlocked, Entity: "provider_profile", EntityID: userID,
		After: map[string]any{"start": b.Start, "end": b.End}, IP: ip,
	})
	return b, err
}

// UnblockDates quita un bloqueo manual (los trabajos no se quitan desde el calendario).
func (s *ProviderService) UnblockDates(ctx context.Context, userID, blockID, ip string) error {
	if _, err := s.profile(ctx, userID); err != nil {
		return err
	}
	return s.d.Providers.DeleteProviderManualBlock(ctx, userID, blockID, domain.AuditEntry{
		ActorID: userID, Action: domain.AuditProviderUnblocked, Entity: "provider_profile", EntityID: userID,
		Before: map[string]any{"block_id": blockID}, IP: ip,
	})
}

// providerAudit guarda lo que cambia del perfil sin el celular (dato privado): basta saber que cambió.
func providerAudit(actorID, action string, next domain.ProviderProfile, before *domain.ProviderProfile, ip string) domain.AuditEntry {
	after := providerSnapshot(next)
	entry := domain.AuditEntry{ActorID: actorID, Action: action, Entity: "provider_profile", EntityID: next.UserID, After: after, IP: ip}
	if before != nil {
		entry.Before = providerSnapshot(*before)
		after["phone_changed"] = before.Phone != next.Phone
	}
	return entry
}

func providerSnapshot(p domain.ProviderProfile) map[string]any {
	type trade struct {
		Category   string         `json:"category_id"`
		HourlyRate domain.Cents   `json:"hourly_rate"`
		MinHours   int            `json:"min_hours"`
		Packages   []domain.Cents `json:"package_prices"`
	}
	trades := make([]trade, len(p.Trades))
	for i, t := range p.Trades {
		trades[i] = trade{Category: t.CategoryID, HourlyRate: t.HourlyRate, MinHours: t.MinHours}
		for _, pkg := range t.Packages {
			trades[i].Packages = append(trades[i].Packages, pkg.Price)
		}
	}
	return map[string]any{
		"status": p.Status, "city_id": p.CityID, "business_name": p.BusinessName, "years_experience": p.YearsExperience,
		"warranty_days": p.WarrantyDays, "accepts_urgent": p.AcceptsUrgent, "trades": trades,
		"coverage_zone_ids": p.CoverageZoneIDs, "weekly_slots": len(p.Weekly),
	}
}
