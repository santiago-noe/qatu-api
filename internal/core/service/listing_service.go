package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// ListingInput es todo lo que el arrendador edita de una publicación (el asistente manda el
// formulario completo en cada guardado). Los distritos de delivery van por ID (GET /cities/{slug}/zones).
type ListingInput struct {
	CategoryID        string
	Title             string
	Description       string
	Attributes        json.RawMessage
	ReplacementValue  domain.Cents
	Deposit           domain.Cents
	Prices            domain.Prices
	Accessories       []string
	UsageInstructions string
	PickupEnabled     bool
	PickupLocation    *domain.GeoPoint
	DeliveryEnabled   bool
	DeliveryFee       domain.Cents
	DeliveryZones     []string
	BookingMode       domain.BookingMode
	CancelPolicy      domain.CancelPolicy
	MinVerification   int
	MinNoticeHours    int
	MinDurationHours  int
	MaxDurationHours  int
}

// Valores iniciales de un borrador (docs/02: reglas por defecto sensatas para el piloto).
const (
	defaultMinNoticeHours   = 12
	defaultMinDurationHours = 24
	defaultMaxDurationHours = 720
)

// ListingDeps agrupa lo que necesita ListingService.
type ListingDeps struct {
	Lenders    port.LenderRepository
	Listings   port.ListingRepository
	Categories port.CategoryFinder
	Catalog    *CatalogService
	Attributes port.AttributesValidator
	Settings   *SettingsResolver
	Clock      port.Clock
	IDs        port.IDGenerator
	// LocationSecret desplaza el punto público (domain.PublicPoint).
	LocationSecret []byte
}

// ListingService: el arrendador crea, edita, envía, pausa, archiva y duplica sus publicaciones y
// maneja su calendario (spec 003). Solo el dueño ve o toca una publicación: para cualquier otro
// no existe (404), así no se revela qué IDs existen.
type ListingService struct {
	d ListingDeps
}

func NewListingService(d ListingDeps) *ListingService { return &ListingService{d: d} }

// listingContext es lo que se resuelve una vez por operación: perfil, ciudad, categoría y ajustes.
type listingContext struct {
	profile  domain.LenderProfile
	city     domain.City
	category domain.Category
	parent   domain.Category
	settings domain.SettingsSnapshot
}

func (s *ListingService) profile(ctx context.Context, ownerID string) (domain.LenderProfile, error) {
	p, err := s.d.Lenders.FindLenderProfile(ctx, ownerID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.LenderProfile{}, domain.ErrLenderProfileRequired
	}
	return p, err
}

// resolve arma el contexto de una categoría para el dueño: la categoría debe poder publicarse en
// su ciudad (activa, no prohibida y de segundo nivel).
func (s *ListingService) resolve(ctx context.Context, profile domain.LenderProfile, categoryID string) (listingContext, error) {
	loc, _, err := locationByIDs(ctx, s.d.Catalog, profile.CityID, profile.ZoneID)
	if err != nil {
		return listingContext{}, err
	}
	if loc.City.ID == "" {
		return listingContext{}, domain.ErrListingOwnerLocation
	}
	lc := listingContext{profile: profile, city: loc.City}
	if lc.category, lc.parent, err = s.publishableCategory(ctx, loc.City, categoryID); err != nil {
		return listingContext{}, err
	}
	lc.settings, err = s.d.Settings.Snapshot(ctx, loc.City.ID, lc.category, domain.ListingSettingKeys(lc.category.RiskLevel)...)
	return lc, err
}

func (s *ListingService) publishableCategory(ctx context.Context, city domain.City, categoryID string) (domain.Category, domain.Category, error) {
	category, err := s.d.Categories.FindCategory(ctx, categoryID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && category.ParentID == "") {
		return domain.Category{}, domain.Category{}, domain.ErrListingCategory
	}
	if err != nil {
		return domain.Category{}, domain.Category{}, err
	}
	parent, err := s.d.Categories.FindCategory(ctx, category.ParentID)
	if err != nil {
		return domain.Category{}, domain.Category{}, err
	}
	if err := domain.CheckListingCategory(category, parent); err != nil {
		return domain.Category{}, domain.Category{}, err
	}
	// Alcance por ciudad: el árbol público de la ciudad ya aplica lo encendido o apagado en ella.
	tree, err := s.d.Catalog.Categories(ctx, domain.VerticalRental, city.Slug)
	if err != nil {
		return domain.Category{}, domain.Category{}, err
	}
	for _, root := range tree {
		for _, child := range root.Children {
			if child.ID == category.ID {
				return category, parent, nil
			}
		}
	}
	return domain.Category{}, domain.Category{}, domain.ErrListingCategory
}

// build aplica el formulario sobre la publicación (nueva o existente) y resuelve lo derivado:
// distrito, punto público, distritos de delivery y valores normalizados.
func (s *ListingService) build(ctx context.Context, lc listingContext, l domain.ToolListing, in ListingInput) (domain.ToolListing, error) {
	l.CategoryID, l.CityID = lc.category.ID, lc.city.ID
	l.Title, l.Description, l.UsageInstructions = in.Title, in.Description, in.UsageInstructions
	l.Attributes = in.Attributes
	if len(bytes.TrimSpace(l.Attributes)) == 0 {
		l.Attributes = json.RawMessage(`{}`)
	}
	if !isJSONObject(l.Attributes) {
		return domain.ToolListing{}, domain.ErrListingAttributes
	}
	l.ReplacementValue, l.Deposit, l.Prices = in.ReplacementValue, in.Deposit, in.Prices
	l.Accessories = in.Accessories
	l.BookingMode, l.CancelPolicy = in.BookingMode, in.CancelPolicy
	l.MinVerification, l.MinNoticeHours = in.MinVerification, in.MinNoticeHours
	l.MinDurationHours, l.MaxDurationHours = in.MinDurationHours, in.MaxDurationHours
	if l.MinVerification < domain.MinVerificationFor(lc.category.RiskLevel) {
		return domain.ToolListing{}, domain.ErrListingVerification
	}

	// Recojo: el distrito sale del punto exacto, que debe estar en la ciudad del arrendador.
	l.PickupEnabled, l.PickupLocation, l.PublicLocation, l.PublicRadiusM = in.PickupEnabled, nil, nil, 0
	l.ZoneID = lc.profile.ZoneID
	if in.PickupEnabled && in.PickupLocation == nil {
		return domain.ToolListing{}, domain.ErrListingPickupLocation
	}
	if in.PickupEnabled {
		if err := in.PickupLocation.Validate(); err != nil {
			return domain.ToolListing{}, err
		}
		loc, err := s.d.Catalog.LocationAt(ctx, *in.PickupLocation)
		if errors.Is(err, domain.ErrOutOfCoverage) || (err == nil && loc.City.ID != lc.city.ID) {
			return domain.ToolListing{}, domain.ErrListingPickupLocation
		}
		if err != nil {
			return domain.ToolListing{}, err
		}
		radius, err := lc.settings.Int(domain.SettingPublicRadius)
		if err != nil {
			return domain.ToolListing{}, err
		}
		exact := *in.PickupLocation
		public := domain.PublicPoint(exact, s.d.LocationSecret, l.ID, int(radius))
		l.PickupLocation, l.PublicLocation, l.PublicRadiusM, l.ZoneID = &exact, &public, int(radius), loc.Zone.ID
	}

	// Delivery: tarifa fija y distritos de la misma ciudad.
	l.DeliveryEnabled, l.DeliveryFee, l.DeliveryZoneIDs = in.DeliveryEnabled, 0, nil
	if in.DeliveryEnabled {
		ids, err := s.cityZoneIDs(ctx, lc.city.Slug, in.DeliveryZones)
		if err != nil {
			return domain.ToolListing{}, err
		}
		l.DeliveryFee, l.DeliveryZoneIDs = in.DeliveryFee, ids
	}
	return domain.NormalizeListing(l)
}

// cityZoneIDs revisa que cada distrito sea de la ciudad y esté activo.
func (s *ListingService) cityZoneIDs(ctx context.Context, citySlug string, ids []string) ([]string, error) {
	zones, err := s.d.Catalog.Zones(ctx, citySlug)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !slices.ContainsFunc(zones, func(z domain.Zone) bool { return z.ID == id }) {
			return nil, domain.ErrListingDeliveryZones
		}
	}
	return ids, nil
}

// checkPublishable son las reglas que debe cumplir una publicación fuera de borrador (al enviarla
// y al editarla ya publicada): atributos según el esquema, garantía en rango y datos completos.
func (s *ListingService) checkPublishable(ctx context.Context, lc listingContext, l domain.ToolListing) error {
	if err := s.d.Attributes.Validate(lc.category.EffectiveSchema(lc.parent), l.Attributes); err != nil {
		return err
	}
	rule, err := domain.DepositRuleFrom(lc.settings, lc.category.RiskLevel)
	if err != nil {
		return err
	}
	if err := domain.CheckDeposit(l, rule); err != nil {
		return err
	}
	photos, err := s.d.Listings.ReadyPhotos(ctx, l.ID)
	if err != nil {
		return err
	}
	return domain.CheckListingComplete(l, photos)
}

// checkDraftDeposit avisa temprano si la garantía escrita ya está fuera de rango.
func checkDraftDeposit(lc listingContext, l domain.ToolListing) error {
	if l.Deposit == 0 {
		return nil
	}
	rule, err := domain.DepositRuleFrom(lc.settings, lc.category.RiskLevel)
	if err != nil {
		return err
	}
	return domain.CheckDeposit(l, rule)
}

// Create guarda un borrador. Pide el perfil de arrendador y una categoría publicable.
func (s *ListingService) Create(ctx context.Context, ownerID string, in ListingInput, ip string) (domain.ToolListing, error) {
	profile, err := s.profile(ctx, ownerID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	lc, err := s.resolve(ctx, profile, in.CategoryID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	applyDraftDefaults(&in, lc.category.RiskLevel)
	l, err := s.build(ctx, lc, domain.ToolListing{ID: s.d.IDs.NewID(), OwnerID: ownerID, Status: domain.ListingDraft}, in)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if err := checkDraftDeposit(lc, l); err != nil {
		return domain.ToolListing{}, err
	}
	if err := s.d.Listings.CreateListing(ctx, l, listingAudit(ownerID, domain.AuditListingCreated, l, nil, ip)); err != nil {
		return domain.ToolListing{}, err
	}
	return s.d.Listings.FindListing(ctx, l.ID)
}

// applyDraftDefaults completa lo que el asistente aún no preguntó.
func applyDraftDefaults(in *ListingInput, risk domain.RiskLevel) {
	if in.BookingMode == "" {
		in.BookingMode = domain.BookingOnRequest
	}
	if in.CancelPolicy == "" {
		in.CancelPolicy = domain.CancelModerate
	}
	in.MinVerification = max(in.MinVerification, domain.MinVerificationFor(risk))
	if in.MinNoticeHours == 0 {
		in.MinNoticeHours = defaultMinNoticeHours
	}
	if in.MinDurationHours == 0 {
		in.MinDurationHours = defaultMinDurationHours
	}
	if in.MaxDurationHours == 0 {
		in.MaxDurationHours = defaultMaxDurationHours
	}
}

// Get devuelve una publicación del dueño.
func (s *ListingService) Get(ctx context.Context, ownerID, id string) (domain.ToolListing, error) {
	l, err := s.d.Listings.FindListing(ctx, id)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if l.OwnerID != ownerID {
		return domain.ToolListing{}, domain.ErrNotFound
	}
	return l, nil
}

// List devuelve las publicaciones del dueño ("Mis publicaciones").
func (s *ListingService) List(ctx context.Context, ownerID string) ([]domain.ToolListing, error) {
	return s.d.Listings.ListOwnerListings(ctx, ownerID)
}

// Update reemplaza los datos. En revisión o archivada no se edita; publicada o pausada debe seguir
// cumpliendo todo (lo que ve el público nunca queda incompleto) y no cambia de categoría.
// Editar precios no toca reservas: cada una guarda su copia (006).
func (s *ListingService) Update(ctx context.Context, ownerID, id string, version int, in ListingInput, ip string) (domain.ToolListing, error) {
	current, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if !current.Status.Editable() {
		return domain.ToolListing{}, domain.ErrListingNotEditable
	}
	live := current.Status == domain.ListingPublished || current.Status == domain.ListingPaused
	if live && in.CategoryID != current.CategoryID {
		return domain.ToolListing{}, domain.ErrListingCategoryLocked
	}
	profile, err := s.profile(ctx, ownerID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	lc, err := s.resolve(ctx, profile, in.CategoryID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	next, err := s.build(ctx, lc, current, in)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if live {
		err = s.checkPublishable(ctx, lc, next)
	} else {
		err = checkDraftDeposit(lc, next)
	}
	if err != nil {
		return domain.ToolListing{}, err
	}
	return s.d.Listings.UpdateListing(ctx, next, version, listingAudit(ownerID, domain.AuditListingUpdated, next, &current, ip))
}

// Submit envía un borrador (o una rechazada ya corregida): pasa por moderación si es la primera
// del arrendador, si la categoría es de riesgo alto o si la habían rechazado.
func (s *ListingService) Submit(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error) {
	current, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return domain.ToolListing{}, err
	}
	profile, err := s.profile(ctx, ownerID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	lc, err := s.resolve(ctx, profile, current.CategoryID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if err := s.checkPublishable(ctx, lc, current); err != nil {
		return domain.ToolListing{}, err
	}
	published, err := s.d.Listings.OwnerHasPublished(ctx, ownerID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	review := current.Status == domain.ListingRejected || domain.ListingNeedsReview(!published, lc.category.RiskLevel)
	return s.transition(ctx, ownerID, current, version, domain.ListingSubmit, review, ip)
}

// Pause saca del catálogo sin perder nada; Resume la vuelve a mostrar si su categoría sigue permitida.
func (s *ListingService) Pause(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error) {
	return s.simpleTransition(ctx, ownerID, id, version, domain.ListingPause, ip)
}

func (s *ListingService) Resume(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error) {
	current, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return domain.ToolListing{}, err
	}
	profile, err := s.profile(ctx, ownerID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	if _, err := s.resolve(ctx, profile, current.CategoryID); err != nil {
		return domain.ToolListing{}, err
	}
	return s.transition(ctx, ownerID, current, version, domain.ListingResume, false, ip)
}

// Archive la saca del catálogo para siempre (las reservas ya hechas siguen su curso en 006).
func (s *ListingService) Archive(ctx context.Context, ownerID, id string, version int, ip string) (domain.ToolListing, error) {
	return s.simpleTransition(ctx, ownerID, id, version, domain.ListingArchive, ip)
}

func (s *ListingService) simpleTransition(ctx context.Context, ownerID, id string, version int, action domain.ListingAction, ip string) (domain.ToolListing, error) {
	current, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return domain.ToolListing{}, err
	}
	return s.transition(ctx, ownerID, current, version, action, false, ip)
}

func (s *ListingService) transition(ctx context.Context, actorID string, current domain.ToolListing, version int,
	action domain.ListingAction, review bool, ip string) (domain.ToolListing, error) {
	next, err := domain.ApplyListingAction(current, action, review, s.d.Clock.Now())
	if err != nil {
		return domain.ToolListing{}, err
	}
	audit := domain.AuditEntry{
		ActorID: actorID, Action: domain.AuditListingStatus, Entity: "listing", EntityID: current.ID,
		Before: map[string]any{"status": current.Status}, After: map[string]any{"status": next.Status, "action": action}, IP: ip,
	}
	return s.d.Listings.UpdateListing(ctx, next, version, audit)
}

// Duplicate crea un borrador igual (otra unidad del mismo modelo). Las fotos se suben de nuevo:
// cada unidad muestra la suya.
func (s *ListingService) Duplicate(ctx context.Context, ownerID, id, ip string) (domain.ToolListing, error) {
	original, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return domain.ToolListing{}, err
	}
	profile, err := s.profile(ctx, ownerID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	lc, err := s.resolve(ctx, profile, original.CategoryID)
	if err != nil {
		return domain.ToolListing{}, err
	}
	copy := domain.DuplicateListing(original)
	copy.ID = s.d.IDs.NewID()
	if copy.PickupLocation != nil {
		radius, err := lc.settings.Int(domain.SettingPublicRadius)
		if err != nil {
			return domain.ToolListing{}, err
		}
		public := domain.PublicPoint(*copy.PickupLocation, s.d.LocationSecret, copy.ID, int(radius))
		copy.PublicLocation, copy.PublicRadiusM = &public, int(radius)
	}
	audit := listingAudit(ownerID, domain.AuditListingCreated, copy, nil, ip)
	audit.After.(map[string]any)["duplicated_from"] = original.ID
	if err := s.d.Listings.CreateListing(ctx, copy, audit); err != nil {
		return domain.ToolListing{}, err
	}
	return s.d.Listings.FindListing(ctx, copy.ID)
}

// DepositSuggestion calcula la garantía sugerida y su rango para una categoría y un valor.
func (s *ListingService) DepositSuggestion(ctx context.Context, ownerID, categoryID string, replacement domain.Cents) (domain.DepositRange, error) {
	if replacement <= 0 || replacement > domain.ListingMaxAmount {
		return domain.DepositRange{}, domain.ErrListingAmount
	}
	profile, err := s.profile(ctx, ownerID)
	if err != nil {
		return domain.DepositRange{}, err
	}
	lc, err := s.resolve(ctx, profile, categoryID)
	if err != nil {
		return domain.DepositRange{}, err
	}
	rule, err := domain.DepositRuleFrom(lc.settings, lc.category.RiskLevel)
	if err != nil {
		return domain.DepositRange{}, err
	}
	return domain.SuggestDeposit(replacement, rule), nil
}

// Calendar devuelve los bloqueos de la publicación en la ventana pedida.
func (s *ListingService) Calendar(ctx context.Context, ownerID, id string, from, to time.Time) ([]domain.AvailabilityBlock, error) {
	if _, err := s.Get(ctx, ownerID, id); err != nil {
		return nil, err
	}
	from, to, err := domain.CalendarWindow(from, to)
	if err != nil {
		return nil, err
	}
	return s.d.Listings.ListBlocks(ctx, id, from, to)
}

// BlockDates bloquea fechas a mano. No se cruza con otro bloqueo ni con una reserva.
func (s *ListingService) BlockDates(ctx context.Context, ownerID, id string, b domain.AvailabilityBlock, ip string) (domain.AvailabilityBlock, error) {
	l, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return domain.AvailabilityBlock{}, err
	}
	if l.Status == domain.ListingArchived {
		return domain.AvailabilityBlock{}, domain.ErrListingNotEditable
	}
	b, err = domain.NormalizeManualBlock(b, s.d.Clock.Now())
	if err != nil {
		return domain.AvailabilityBlock{}, err
	}
	b.ListingID, b.CreatedBy = id, ownerID
	b.ID, err = s.d.Listings.AddBlock(ctx, b, domain.AuditEntry{
		ActorID: ownerID, Action: domain.AuditListingBlocked, Entity: "listing",
		After: map[string]any{"start": b.Start, "end": b.End}, IP: ip,
	})
	return b, err
}

// UnblockDates quita un bloqueo manual (las reservas no se quitan desde el calendario).
func (s *ListingService) UnblockDates(ctx context.Context, ownerID, id, blockID, ip string) error {
	if _, err := s.Get(ctx, ownerID, id); err != nil {
		return err
	}
	return s.d.Listings.DeleteManualBlock(ctx, id, blockID, domain.AuditEntry{
		ActorID: ownerID, Action: domain.AuditListingUnblocked, Entity: "listing", EntityID: id,
		Before: map[string]any{"block_id": blockID}, IP: ip,
	})
}

// listingAudit guarda lo que cambia de una publicación sin el punto exacto (dato privado).
func listingAudit(actorID, action string, next domain.ToolListing, before *domain.ToolListing, ip string) domain.AuditEntry {
	entry := domain.AuditEntry{ActorID: actorID, Action: action, Entity: "listing", EntityID: next.ID,
		After: listingSnapshot(next), IP: ip}
	if before != nil {
		entry.Before = listingSnapshot(*before)
	}
	return entry
}

func listingSnapshot(l domain.ToolListing) map[string]any {
	return map[string]any{
		"status": l.Status, "category_id": l.CategoryID, "title": l.Title, "zone_id": l.ZoneID,
		"replacement_value": l.ReplacementValue, "deposit": l.Deposit,
		"prices": []domain.Cents{l.Prices.Hour, l.Prices.Day, l.Prices.Weekend, l.Prices.Week, l.Prices.Month},
		"pickup": l.PickupEnabled, "delivery": l.DeliveryEnabled, "delivery_fee": l.DeliveryFee,
		"booking_mode": l.BookingMode, "cancel_policy": l.CancelPolicy, "min_verification": l.MinVerification,
	}
}

func isJSONObject(raw json.RawMessage) bool {
	var v map[string]any
	return json.Unmarshal(raw, &v) == nil && v != nil
}
