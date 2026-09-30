package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// memoryLenders guarda perfiles y cuenta activaciones.
type memoryLenders struct {
	profiles    map[string]domain.LenderProfile
	activations []port.LenderActivation
}

func (m *memoryLenders) FindLenderProfile(_ context.Context, userID string) (domain.LenderProfile, error) {
	p, ok := m.profiles[userID]
	if !ok {
		return domain.LenderProfile{}, domain.ErrNotFound
	}
	return p, nil
}

func (m *memoryLenders) SaveLenderProfile(_ context.Context, a port.LenderActivation) error {
	m.profiles[a.Profile.UserID] = a.Profile
	m.activations = append(m.activations, a)
	return nil
}

// memoryListings imita al repositorio: versión optimista y bloqueos sin cruces.
type memoryListings struct {
	listings  map[string]domain.ToolListing
	blocks    []domain.AvailabilityBlock
	photos    map[string]int
	published map[string]bool
	audits    []domain.AuditEntry
}

func newMemoryListings() *memoryListings {
	return &memoryListings{listings: map[string]domain.ToolListing{}, photos: map[string]int{}, published: map[string]bool{}}
}

func (m *memoryListings) CreateListing(_ context.Context, l domain.ToolListing, a domain.AuditEntry) error {
	l.Version = 1
	m.listings[l.ID] = l
	m.audits = append(m.audits, a)
	return nil
}

func (m *memoryListings) FindListing(_ context.Context, id string) (domain.ToolListing, error) {
	l, ok := m.listings[id]
	if !ok {
		return domain.ToolListing{}, domain.ErrNotFound
	}
	return l, nil
}

func (m *memoryListings) ListOwnerListings(_ context.Context, ownerID string) ([]domain.ToolListing, error) {
	var out []domain.ToolListing
	for _, l := range m.listings {
		if l.OwnerID == ownerID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (m *memoryListings) UpdateListing(_ context.Context, l domain.ToolListing, version int, a domain.AuditEntry) (domain.ToolListing, error) {
	current, ok := m.listings[l.ID]
	if !ok {
		return domain.ToolListing{}, domain.ErrNotFound
	}
	if current.Version != version {
		return domain.ToolListing{}, domain.ErrListingVersion
	}
	l.Version = version + 1
	m.listings[l.ID] = l
	if l.FirstPublishedAt != nil {
		m.published[l.OwnerID] = true
	}
	m.audits = append(m.audits, a)
	return l, nil
}

func (m *memoryListings) OwnerHasPublished(_ context.Context, ownerID string) (bool, error) {
	return m.published[ownerID], nil
}

func (m *memoryListings) ReadyPhotos(_ context.Context, id string) (int, error) {
	return m.photos[id], nil
}

func (m *memoryListings) ReviewQueue(_ context.Context, limit int) ([]domain.ReviewItem, error) {
	var out []domain.ReviewItem
	for _, l := range m.listings {
		if l.Status == domain.ListingInReview {
			out = append(out, domain.ReviewItem{Listing: l, OwnerName: "Ana", FirstListing: !m.published[l.OwnerID]})
		}
	}
	slices.SortFunc(out, func(a, b domain.ReviewItem) int { return a.Listing.UpdatedAt.Compare(b.Listing.UpdatedAt) })
	return out[:min(limit, len(out))], nil
}

func (m *memoryListings) ListBlocks(_ context.Context, id string, from, to time.Time) ([]domain.AvailabilityBlock, error) {
	var out []domain.AvailabilityBlock
	for _, b := range m.blocks {
		if b.ListingID == id && b.Start.Before(to) && b.End.After(from) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *memoryListings) AddBlock(_ context.Context, b domain.AvailabilityBlock, a domain.AuditEntry) (string, error) {
	if _, err := m.ListBlocks(context.Background(), b.ListingID, b.Start, b.End); err != nil {
		return "", err
	}
	if overlap, _ := m.ListBlocks(context.Background(), b.ListingID, b.Start, b.End); len(overlap) > 0 {
		return "", domain.ErrAvailabilityOverlap
	}
	b.ID = "b" + string(rune('0'+len(m.blocks)))
	m.blocks = append(m.blocks, b)
	m.audits = append(m.audits, a)
	return b.ID, nil
}

func (m *memoryListings) DeleteManualBlock(_ context.Context, listingID, blockID string, a domain.AuditEntry) error {
	i := slices.IndexFunc(m.blocks, func(b domain.AvailabilityBlock) bool {
		return b.ID == blockID && b.ListingID == listingID && b.Reason == domain.BlockManual
	})
	if i < 0 {
		return domain.ErrNotFound
	}
	m.blocks = slices.Delete(m.blocks, i, i+1)
	m.audits = append(m.audits, a)
	return nil
}

// requiredAttributes rechaza atributos vacíos si el esquema pide campos (el adaptador real tiene sus pruebas).
type requiredAttributes struct{}

func (requiredAttributes) Validate(schema, doc []byte) error {
	if bytes.Contains(schema, []byte(`"required"`)) && bytes.Equal(doc, []byte(`{}`)) {
		return domain.ErrListingAttributes
	}
	return nil
}

type listingFixture struct {
	svc      *ListingService
	lenders  *LenderService
	accounts *memoryAccounts
	profiles *memoryLenders
	listings *memoryListings
	catalog  *memoryCatalogAdmin
	clock    *fakeClock
}

var plaza = domain.GeoPoint{Lat: -13.1631, Lng: -74.2237}

func newListingFixture() listingFixture {
	catalog, _, _ := newCatalogFixture() // árbol: Construcción (c1) > Rotomartillo (t1); distrito carmen-alto (z1)
	admin := newMemoryCatalogAdmin()
	admin.categories["t1"] = domain.Category{ID: "t1", ParentID: "c1", Vertical: domain.VerticalRental, Slug: "rotomartillo",
		AttributesSchema: json.RawMessage(`{"type":"object","required":["marca"]}`), RiskLevel: domain.RiskMedium, Enabled: true}
	for key, value := range map[string]string{
		"listings.deposit_medium_bps": "3000", "listings.deposit_high_bps": "5000", domain.SettingDepositMinFactor: "5000",
		domain.SettingDepositMaxFactor: "15000", domain.SettingPublicRadius: "500",
	} {
		admin.settings[settingKey(key, "", "")] = domain.Setting{ID: key, Key: key, Value: json.RawMessage(value), Version: 1}
	}
	clock := &fakeClock{now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	accounts := newMemoryAccounts()
	verified := clock.now
	accounts.users["ana"] = domain.User{ID: "ana", EmailVerifiedAt: &verified, Status: domain.UserActive}
	accounts.users["beto"] = domain.User{ID: "beto", Status: domain.UserActive}
	profiles := &memoryLenders{profiles: map[string]domain.LenderProfile{}}
	listings := newMemoryListings()
	svc := NewListingService(ListingDeps{
		Lenders: profiles, Listings: listings, Categories: admin, Catalog: catalog, Attributes: requiredAttributes{},
		Settings: NewSettingsResolver(admin, clock), Clock: clock, IDs: &seqIDs{}, LocationSecret: []byte("secreto"),
	})
	lenders := NewLenderService(accounts, profiles, catalog, clock, "v1")
	return listingFixture{svc: svc, lenders: lenders, accounts: accounts, profiles: profiles, listings: listings, catalog: admin, clock: clock}
}

func draftInput() ListingInput {
	here := plaza
	return ListingInput{
		CategoryID: "t1", Title: "Rotomartillo Bosch 800 W", Attributes: json.RawMessage(`{"marca":"Bosch"}`),
		ReplacementValue: 450_00, Deposit: 140_00, Prices: domain.Prices{Day: 35_00},
		Accessories: []string{"Maletín"}, PickupEnabled: true, PickupLocation: &here,
		DeliveryEnabled: true, DeliveryFee: 10_00, DeliveryZones: []string{"z1"},
	}
}

// fullInput es el formulario completo que manda el asistente al guardar (con las reglas).
func fullInput() ListingInput {
	in := draftInput()
	in.BookingMode, in.CancelPolicy, in.MinVerification = domain.BookingOnRequest, domain.CancelModerate, 1
	in.MinNoticeHours, in.MinDurationHours, in.MaxDurationHours = 12, 24, 720
	return in
}

func (f listingFixture) activate(t *testing.T) {
	t.Helper()
	if _, err := f.lenders.Save(context.Background(), "ana", LenderInput{Kind: domain.LenderPerson, Phone: "987654321",
		CitySlug: "ayacucho", ZoneSlug: "carmen-alto", AcceptTerms: true}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestLenderActivation(t *testing.T) {
	f := newListingFixture()
	ctx := context.Background()
	in := LenderInput{Kind: domain.LenderPerson, Phone: "987 654 321", CitySlug: "ayacucho", ZoneSlug: "carmen-alto"}

	if _, err := f.lenders.Save(ctx, "beto", in, ""); !errors.Is(err, domain.ErrLenderEmailUnverified) {
		t.Fatalf("sin correo verificado: %v", err)
	}
	if _, err := f.lenders.Save(ctx, "ana", in, ""); !errors.Is(err, domain.ErrLenderTermsRequired) {
		t.Fatalf("la primera vez acepta las condiciones: %v", err)
	}
	bad := in
	bad.AcceptTerms, bad.ZoneSlug = true, "miraflores"
	if _, err := f.lenders.Save(ctx, "ana", bad, ""); !errors.Is(err, domain.ErrInvalidLocation) {
		t.Fatalf("distrito que no atendemos: %v", err)
	}

	in.AcceptTerms = true
	view, err := f.lenders.Save(ctx, "ana", in, "190.1.2.3")
	if err != nil || view.Profile.Phone != "+51987654321" || view.Location.Zone.Slug != "carmen-alto" {
		t.Fatalf("Save = %+v, %v", view, err)
	}
	first := f.profiles.activations[0]
	if first.Consent == nil || first.Consent.Purpose != domain.ConsentLenderTerms || first.Audit.Action != domain.AuditLenderActivated {
		t.Fatalf("la activación guarda el consentimiento y se audita: %+v", first)
	}
	if _, ok := first.Audit.After.(map[string]any)["phone"]; ok {
		t.Fatal("el celular no va a la auditoría")
	}

	// Editar: sin volver a aceptar; si nada cambia, no se escribe.
	in.AcceptTerms = false
	if _, err := f.lenders.Save(ctx, "ana", in, ""); err != nil || len(f.profiles.activations) != 1 {
		t.Fatalf("sin cambios no se escribe: %v", err)
	}
	in.Kind, in.BusinessName = domain.LenderBusiness, "Ferretería El Maestro"
	if _, err := f.lenders.Save(ctx, "ana", in, ""); err != nil {
		t.Fatal(err)
	}
	if last := f.profiles.activations[1]; last.Consent != nil || last.Audit.Action != domain.AuditLenderUpdated {
		t.Fatalf("editar no pide consentimiento de nuevo: %+v", last)
	}
	if _, ok, _ := f.lenders.Get(ctx, "beto"); ok {
		t.Fatal("beto no es arrendador")
	}
}

func TestCreateListing(t *testing.T) {
	f := newListingFixture()
	ctx := context.Background()
	if _, err := f.svc.Create(ctx, "ana", draftInput(), ""); !errors.Is(err, domain.ErrLenderProfileRequired) {
		t.Fatalf("sin perfil de arrendador: %v", err)
	}
	f.activate(t)

	l, err := f.svc.Create(ctx, "ana", draftInput(), "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != domain.ListingDraft || l.CityID != "city-1" || l.ZoneID != "z-ayacucho" || l.OwnerID != "ana" {
		t.Fatalf("borrador en la ciudad del arrendador y el distrito del punto: %+v", l)
	}
	if l.BookingMode != domain.BookingOnRequest || l.CancelPolicy != domain.CancelModerate || l.MinNoticeHours != 12 || l.MinVerification != 1 {
		t.Fatalf("reglas por defecto: %+v", l)
	}
	if l.PublicLocation == nil || *l.PublicLocation == plaza || l.PublicRadiusM != 500 {
		t.Fatalf("el punto público se desplaza: %+v", l.PublicLocation)
	}
	if d := domain.DistanceMeters(plaza, *l.PublicLocation); d > 500 {
		t.Fatalf("dentro del radio: %.0f m", d)
	}
	if !slices.Equal(l.DeliveryZoneIDs, []string{"z1"}) {
		t.Fatalf("distritos de delivery por ID: %v", l.DeliveryZoneIDs)
	}
	if f.listings.audits[0].Action != domain.AuditListingCreated {
		t.Fatal("la creación se audita")
	}
	if _, ok := f.listings.audits[0].After.(map[string]any)["pickup_location"]; ok {
		t.Fatal("el punto exacto no va a la auditoría")
	}

	rejects := []struct {
		name   string
		change func(*ListingInput)
		want   error
	}{
		{"una raíz", func(in *ListingInput) { in.CategoryID = "c1" }, domain.ErrListingCategory},
		{"no existe", func(in *ListingInput) { in.CategoryID = "x" }, domain.ErrListingCategory},
		{"recojo sin punto", func(in *ListingInput) { in.PickupLocation = nil }, domain.ErrListingPickupLocation},
		{"punto fuera de cobertura", func(in *ListingInput) { in.PickupLocation = &domain.GeoPoint{Lat: -12, Lng: -77} }, domain.ErrListingPickupLocation},
		{"distrito de delivery desconocido", func(in *ListingInput) { in.DeliveryZones = []string{"z-miraflores"} }, domain.ErrListingDeliveryZones},
		{"garantía fuera de rango", func(in *ListingInput) { in.Deposit = 500_00 }, domain.ErrListingDeposit},
		{"verificación menor a la del riesgo", func(in *ListingInput) { in.MinVerification = 0; in.BookingMode = domain.BookingInstant }, nil},
		{"atributos que no son objeto", func(in *ListingInput) { in.Attributes = json.RawMessage(`[1]`) }, domain.ErrListingAttributes},
	}
	for _, tt := range rejects {
		in := draftInput()
		tt.change(&in)
		_, err := f.svc.Create(ctx, "ana", in, "")
		if tt.want == nil {
			if err != nil {
				t.Errorf("%s: el mínimo se completa solo al crear: %v", tt.name, err)
			}
			continue
		}
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: quiero %v, llegó %v", tt.name, tt.want, err)
		}
	}

	prohibited := f.catalog.categories["t1"]
	prohibited.Prohibited = true
	f.catalog.categories["t1"] = prohibited
	if _, err := f.svc.Create(ctx, "ana", draftInput(), ""); !errors.Is(err, domain.ErrListingProhibited) {
		t.Fatalf("categoría prohibida: %v", err)
	}
}

func TestListingLifecycle(t *testing.T) {
	f := newListingFixture()
	ctx := context.Background()
	f.activate(t)
	l, err := f.svc.Create(ctx, "ana", draftInput(), "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Submit(ctx, "ana", l.ID, l.Version, ""); !errors.Is(err, domain.ErrListingPhotos) {
		t.Fatalf("sin 3 fotos no se envía: %v", err)
	}
	f.listings.photos[l.ID] = 3
	noAttrs := fullInput()
	noAttrs.Attributes = nil
	if l, err = f.svc.Update(ctx, "ana", l.ID, l.Version, noAttrs, ""); err != nil {
		t.Fatalf("un borrador guarda atributos incompletos: %v", err)
	}
	if _, err := f.svc.Submit(ctx, "ana", l.ID, l.Version, ""); !errors.Is(err, domain.ErrListingAttributes) {
		t.Fatalf("al enviar se validan los atributos: %v", err)
	}
	if l, err = f.svc.Update(ctx, "ana", l.ID, l.Version, fullInput(), ""); err != nil {
		t.Fatal(err)
	}

	// Primera publicación del arrendador: a revisión, y congelada mientras tanto.
	l, err = f.svc.Submit(ctx, "ana", l.ID, l.Version, "")
	if err != nil || l.Status != domain.ListingInReview {
		t.Fatalf("la primera va a revisión: %+v, %v", l.Status, err)
	}
	if _, err := f.svc.Update(ctx, "ana", l.ID, l.Version, fullInput(), ""); !errors.Is(err, domain.ErrListingNotEditable) {
		t.Fatalf("en revisión no se edita: %v", err)
	}

	// Aprobada por moderación (parte 4): la simula el repositorio.
	approved := f.listings.listings[l.ID]
	now := f.clock.now
	approved.Status, approved.FirstPublishedAt = domain.ListingPublished, &now
	f.listings.listings[l.ID] = approved
	f.listings.published["ana"] = true

	// La siguiente de riesgo medio se publica directo.
	second, _ := f.svc.Create(ctx, "ana", draftInput(), "")
	f.listings.photos[second.ID] = 3
	second, err = f.svc.Submit(ctx, "ana", second.ID, second.Version, "")
	if err != nil || second.Status != domain.ListingPublished || second.FirstPublishedAt == nil {
		t.Fatalf("sin revisión y con fecha de publicación: %+v, %v", second, err)
	}

	// Publicada: se edita si sigue completa, pero no cambia de categoría.
	edited := fullInput()
	edited.Prices.Day = 40_00
	if second, err = f.svc.Update(ctx, "ana", second.ID, second.Version, edited, ""); err != nil || second.Prices.Day != 40_00 {
		t.Fatalf("editar precios de una publicada: %v", err)
	}
	edited.CategoryID = "otra"
	if _, err := f.svc.Update(ctx, "ana", second.ID, second.Version, edited, ""); !errors.Is(err, domain.ErrListingCategoryLocked) {
		t.Fatalf("categoría fija tras publicar: %v", err)
	}
	incomplete := fullInput()
	incomplete.Prices.Day = 0
	if _, err := f.svc.Update(ctx, "ana", second.ID, second.Version, incomplete, ""); !errors.Is(err, domain.ErrListingDayPrice) {
		t.Fatalf("una publicada no queda incompleta: %v", err)
	}

	// Versión vieja: alguien la cambió en otra pestaña.
	if _, err := f.svc.Pause(ctx, "ana", second.ID, second.Version-1, ""); !errors.Is(err, domain.ErrListingVersion) {
		t.Fatalf("versión vieja: %v", err)
	}
	second, err = f.svc.Pause(ctx, "ana", second.ID, second.Version, "")
	if err != nil || second.Status != domain.ListingPaused {
		t.Fatalf("pausar: %v", err)
	}
	// Mientras está pausada, prohíben la categoría: no puede volver.
	prohibited := f.catalog.categories["t1"]
	prohibited.Prohibited = true
	f.catalog.categories["t1"] = prohibited
	if _, err := f.svc.Resume(ctx, "ana", second.ID, second.Version, ""); !errors.Is(err, domain.ErrListingProhibited) {
		t.Fatalf("reanudar en una categoría prohibida: %v", err)
	}
	second, err = f.svc.Archive(ctx, "ana", second.ID, second.Version, "")
	if err != nil || second.Status != domain.ListingArchived {
		t.Fatalf("archivar: %v", err)
	}

	if _, err := f.svc.Get(ctx, "beto", second.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("otra persona no ve publicaciones ajenas")
	}
	if last := f.listings.audits[len(f.listings.audits)-1]; last.Action != domain.AuditListingStatus {
		t.Fatalf("cada cambio de estado se audita: %+v", last)
	}
}

func TestDuplicateListingService(t *testing.T) {
	f := newListingFixture()
	ctx := context.Background()
	f.activate(t)
	original, _ := f.svc.Create(ctx, "ana", draftInput(), "")
	copy, err := f.svc.Duplicate(ctx, "ana", original.ID, "")
	if err != nil || copy.ID == original.ID || copy.Status != domain.ListingDraft || copy.Title != original.Title+" (copia)" {
		t.Fatalf("Duplicate = %+v, %v", copy, err)
	}
	if *copy.PublicLocation == *original.PublicLocation {
		t.Fatal("cada unidad tiene su propio punto público (sale de su ID)")
	}
	if f.listings.audits[len(f.listings.audits)-1].After.(map[string]any)["duplicated_from"] != original.ID {
		t.Fatal("la auditoría dice de cuál se copió")
	}
}

func TestListingCalendar(t *testing.T) {
	f := newListingFixture()
	ctx := context.Background()
	f.activate(t)
	l, _ := f.svc.Create(ctx, "ana", draftInput(), "")
	day := 24 * time.Hour
	start := f.clock.now.Add(2 * day)

	b, err := f.svc.BlockDates(ctx, "ana", l.ID, domain.AvailabilityBlock{Start: start, End: start.Add(3 * day), Note: "taller"}, "")
	if err != nil || b.ID == "" || b.Reason != domain.BlockManual {
		t.Fatalf("BlockDates = %+v, %v", b, err)
	}
	if _, err := f.svc.BlockDates(ctx, "ana", l.ID, domain.AvailabilityBlock{Start: start.Add(day), End: start.Add(5 * day)}, ""); !errors.Is(err, domain.ErrAvailabilityOverlap) {
		t.Fatalf("fechas que se cruzan: %v", err)
	}
	blocks, err := f.svc.Calendar(ctx, "ana", l.ID, f.clock.now, f.clock.now.AddDate(0, 1, 0))
	if err != nil || len(blocks) != 1 {
		t.Fatalf("Calendar = %+v, %v", blocks, err)
	}
	if _, err := f.svc.Calendar(ctx, "ana", l.ID, f.clock.now, f.clock.now.AddDate(3, 0, 0)); !errors.Is(err, domain.ErrAvailabilityPeriod) {
		t.Fatal("ventana demasiado larga")
	}
	if err := f.svc.UnblockDates(ctx, "ana", l.ID, b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnblockDates(ctx, "beto", l.ID, b.ID, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("otra persona no toca el calendario")
	}
}

func TestDepositSuggestionService(t *testing.T) {
	f := newListingFixture()
	ctx := context.Background()
	f.activate(t)
	r, err := f.svc.DepositSuggestion(ctx, "ana", "t1", 450_00)
	if err != nil || r != (domain.DepositRange{Suggested: 140_00, Min: 70_00, Max: 210_00}) {
		t.Fatalf("riesgo medio: 30 %% de S/ 450: %+v, %v", r, err)
	}
	if _, err := f.svc.DepositSuggestion(ctx, "ana", "t1", 0); !errors.Is(err, domain.ErrListingAmount) {
		t.Fatal("sin valor no hay sugerencia")
	}
}

// Los atributos se definen en la raíz (Construcción) y rigen para sus tipos.
func TestSubmitValidatesInheritedAttributes(t *testing.T) {
	f := newListingFixture()
	ctx := context.Background()
	root := f.catalog.categories["c1"]
	root.AttributesSchema = json.RawMessage(`{"type":"object","properties":{"marca":{"type":"string"}},"required":["marca"]}`)
	f.catalog.categories["c1"] = root
	leaf := f.catalog.categories["t1"]
	leaf.AttributesSchema = json.RawMessage(`{"type":"object","properties":{}}`)
	f.catalog.categories["t1"] = leaf
	f.activate(t)

	in := fullInput()
	in.Attributes = json.RawMessage(`{}`)
	l, err := f.svc.Create(ctx, "ana", in, "")
	if err != nil {
		t.Fatal(err)
	}
	f.listings.photos[l.ID] = 3
	if _, err := f.svc.Submit(ctx, "ana", l.ID, l.Version, ""); !errors.Is(err, domain.ErrListingAttributes) {
		t.Fatalf("la marca que pide Construcción es obligatoria en Rotomartillo: %v", err)
	}
}

func TestPublicCatalogUsesEffectiveSchemas(t *testing.T) {
	tree := withEffectiveSchemas([]domain.Category{{
		ID: "c1", AttributesSchema: json.RawMessage(`{"type":"object","properties":{"marca":{"type":"string"}}}`),
		Children: []domain.Category{{ID: "t1", ParentID: "c1", AttributesSchema: json.RawMessage(`{"type":"object","properties":{}}`)}},
	}})
	if !strings.Contains(string(tree[0].Children[0].AttributesSchema), "marca") {
		t.Fatalf("el tipo trae los atributos de su raíz: %s", tree[0].Children[0].AttributesSchema)
	}
}
