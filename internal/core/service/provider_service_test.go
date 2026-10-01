package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

// memoryProviders imita al repositorio: versión optimista y bloqueos sin cruces.
type memoryProviders struct {
	profiles map[string]domain.ProviderProfile
	consents []domain.Consent
	blocks   []domain.AvailabilityBlock
	audits   []domain.AuditEntry
}

func (m *memoryProviders) FindProvider(_ context.Context, userID string) (domain.ProviderProfile, error) {
	p, ok := m.profiles[userID]
	if !ok {
		return domain.ProviderProfile{}, domain.ErrNotFound
	}
	return p, nil
}

func (m *memoryProviders) CreateProvider(_ context.Context, p domain.ProviderProfile, c domain.Consent, a domain.AuditEntry) error {
	p.Version = 1
	m.profiles[p.UserID] = p
	m.consents = append(m.consents, c)
	m.audits = append(m.audits, a)
	return nil
}

func (m *memoryProviders) UpdateProvider(_ context.Context, p domain.ProviderProfile, version int, a domain.AuditEntry) (domain.ProviderProfile, error) {
	current, ok := m.profiles[p.UserID]
	if !ok {
		return domain.ProviderProfile{}, domain.ErrNotFound
	}
	if current.Version != version {
		return domain.ProviderProfile{}, domain.ErrProviderVersion
	}
	p.Version = version + 1
	m.profiles[p.UserID] = p
	m.audits = append(m.audits, a)
	return p, nil
}

func (m *memoryProviders) ListProviderBlocks(_ context.Context, providerID string, from, to time.Time) ([]domain.AvailabilityBlock, error) {
	var out []domain.AvailabilityBlock
	for _, b := range m.blocks {
		if b.CreatedBy == providerID && b.Start.Before(to) && b.End.After(from) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *memoryProviders) AddProviderBlock(ctx context.Context, providerID string, b domain.AvailabilityBlock, a domain.AuditEntry) (string, error) {
	if overlap, _ := m.ListProviderBlocks(ctx, providerID, b.Start, b.End); len(overlap) > 0 {
		return "", domain.ErrAvailabilityOverlap
	}
	b.ID = "pb" + string(rune('0'+len(m.blocks)))
	m.blocks = append(m.blocks, b)
	m.audits = append(m.audits, a)
	return b.ID, nil
}

func (m *memoryProviders) DeleteProviderManualBlock(_ context.Context, providerID, blockID string, a domain.AuditEntry) error {
	i := slices.IndexFunc(m.blocks, func(b domain.AvailabilityBlock) bool {
		return b.ID == blockID && b.CreatedBy == providerID && b.Reason == domain.BlockManual
	})
	if i < 0 {
		return domain.ErrNotFound
	}
	m.blocks = slices.Delete(m.blocks, i, i+1)
	m.audits = append(m.audits, a)
	return nil
}

type providerFixture struct {
	svc       *ProviderService
	providers *memoryProviders
	clock     *fakeClock
}

func newProviderFixture() providerFixture {
	catalog, _, _ := newCatalogFixture() // oficio raíz c1 en ayacucho; distrito z1
	clock := &fakeClock{now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	accounts := newMemoryAccounts()
	verified := clock.now
	accounts.users["ana"] = domain.User{ID: "ana", EmailVerifiedAt: &verified, Status: domain.UserActive}
	accounts.users["beto"] = domain.User{ID: "beto", Status: domain.UserActive}
	providers := &memoryProviders{profiles: map[string]domain.ProviderProfile{}}
	svc := NewProviderService(ProviderDeps{
		Accounts: accounts, Providers: providers, Catalog: catalog, Clock: clock, IDs: &seqIDs{}, TermsVersion: "v1",
	})
	return providerFixture{svc: svc, providers: providers, clock: clock}
}

// providerInput es un perfil completo: un oficio con tarifa y un paquete, un distrito y una franja.
func providerInput() ProviderInput {
	return ProviderInput{
		Phone: "987654321", CitySlug: "ayacucho", Bio: "Gasfitero con 8 años arreglando termas y fugas en casas.",
		YearsExperience: 8, AcceptTerms: true,
		Trades: []domain.ProviderTrade{{CategoryID: "c1", HourlyRate: 25_00, MinHours: 2, Packages: []domain.ServicePackage{
			{Title: "Cambio de grifería", Price: 60_00, DurationMinutes: 60},
		}}},
		CoverageZoneIDs: []string{"z1"},
		Weekly:          []domain.WeeklySlot{{Weekday: 1, Start: 8 * 60, End: 17 * 60}},
	}
}

func TestProviderActivate(t *testing.T) {
	f := newProviderFixture()
	ctx := context.Background()

	if _, err := f.svc.Activate(ctx, "beto", providerInput(), ""); !errors.Is(err, domain.ErrProviderEmailUnverified) {
		t.Fatalf("correo sin verificar: %v", err)
	}
	in := providerInput()
	in.AcceptTerms = false
	if _, err := f.svc.Activate(ctx, "ana", in, ""); !errors.Is(err, domain.ErrProviderTermsRequired) {
		t.Fatalf("sin condiciones: %v", err)
	}

	view, err := f.svc.Activate(ctx, "ana", providerInput(), "190.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	p := view.Profile
	if p.Status != domain.ListingDraft || p.WarrantyDays != domain.DefaultWarrantyDays || p.Phone != "+51987654321" {
		t.Fatalf("borrador con garantía sugerida: %+v", p)
	}
	if view.City.Slug != "ayacucho" || p.Trades[0].Packages[0].ID == "" {
		t.Fatalf("ciudad y paquete con ID: %+v", view)
	}
	if len(f.providers.consents) != 1 || f.providers.consents[0].Purpose != domain.ConsentProviderTerms {
		t.Fatalf("consentimiento: %+v", f.providers.consents)
	}
	if f.providers.audits[0].Action != domain.AuditProviderActivated {
		t.Fatalf("auditoría: %+v", f.providers.audits[0])
	}
	if _, err := f.svc.Activate(ctx, "ana", providerInput(), ""); !errors.Is(err, domain.ErrProviderExists) {
		t.Fatalf("un perfil por persona: %v", err)
	}
}

func TestProviderCatalogChecks(t *testing.T) {
	f := newProviderFixture()
	ctx := context.Background()
	cases := map[string]struct {
		edit func(*ProviderInput)
		want error
	}{
		"ciudad":             {func(in *ProviderInput) { in.CitySlug = "cusco" }, domain.ErrInvalidLocation},
		"tipo, no oficio":    {func(in *ProviderInput) { in.Trades[0].CategoryID = "t1" }, domain.ErrProviderTrade},
		"oficio inexistente": {func(in *ProviderInput) { in.Trades[0].CategoryID = "x" }, domain.ErrProviderTrade},
		"distrito ajeno":     {func(in *ProviderInput) { in.CoverageZoneIDs = []string{"z9"} }, domain.ErrProviderCoverage},
	}
	for name, c := range cases {
		in := providerInput()
		c.edit(&in)
		if _, err := f.svc.Activate(ctx, "ana", in, ""); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestProviderUpdateKeepsPackageIDs(t *testing.T) {
	f := newProviderFixture()
	ctx := context.Background()
	view, _ := f.svc.Activate(ctx, "ana", providerInput(), "")
	kept := view.Profile.Trades[0].Packages[0].ID

	in := providerInput()
	in.Trades[0].Packages[0].ID = kept
	in.Trades[0].Packages = append(in.Trades[0].Packages,
		domain.ServicePackage{ID: "ajeno", Title: "Destape de desagüe", Price: 50_00, DurationMinutes: 90})
	view, err := f.svc.Update(ctx, "ana", 1, in, "")
	if err != nil {
		t.Fatal(err)
	}
	pkgs := view.Profile.Trades[0].Packages
	if pkgs[0].ID != kept || pkgs[1].ID == "ajeno" || pkgs[1].ID == "" {
		t.Fatalf("se conserva el ID propio y se ignora uno ajeno: %+v", pkgs)
	}
	if _, err := f.svc.Update(ctx, "ana", 1, in, ""); !errors.Is(err, domain.ErrProviderVersion) {
		t.Fatalf("versión vieja: %v", err)
	}
	if _, err := f.svc.Update(ctx, "beto", 1, in, ""); !errors.Is(err, domain.ErrProviderProfileRequired) {
		t.Fatalf("sin perfil: %v", err)
	}
}

func TestProviderSubmitAndLifecycle(t *testing.T) {
	f := newProviderFixture()
	ctx := context.Background()

	in := providerInput()
	in.Bio, in.Weekly = "", nil
	view, _ := f.svc.Activate(ctx, "ana", in, "")
	if _, err := f.svc.Submit(ctx, "ana", view.Profile.Version, ""); !errors.Is(err, domain.ErrProviderBioRequired) {
		t.Fatalf("incompleto no se envía: %v", err)
	}
	view, _ = f.svc.Update(ctx, "ana", view.Profile.Version, providerInput(), "")
	view, err := f.svc.Submit(ctx, "ana", view.Profile.Version, "")
	if err != nil || view.Profile.Status != domain.ListingInReview {
		t.Fatalf("sin nivel P va a revisión: %v %v", view.Profile.Status, err)
	}
	if _, err := f.svc.Update(ctx, "ana", view.Profile.Version, providerInput(), ""); !errors.Is(err, domain.ErrProviderNotEditable) {
		t.Fatalf("en revisión no se edita: %v", err)
	}

	// Moderación lo aprueba (tramo 2); aquí se simula.
	p := f.providers.profiles["ana"]
	now := f.clock.now
	p.Status, p.VerifiedAt, p.FirstPublishedAt = domain.ListingPublished, &now, &now
	f.providers.profiles["ana"] = p

	// Publicado: lo que se guarda debe seguir completo.
	broken := providerInput()
	broken.CoverageZoneIDs = nil
	if _, err := f.svc.Update(ctx, "ana", p.Version, broken, ""); !errors.Is(err, domain.ErrProviderCoverage) {
		t.Fatalf("publicado no queda incompleto: %v", err)
	}
	view, err = f.svc.Pause(ctx, "ana", p.Version, "")
	if err != nil || view.Profile.Status != domain.ListingPaused {
		t.Fatalf("pausar: %v %v", view.Profile.Status, err)
	}
	view, err = f.svc.Resume(ctx, "ana", view.Profile.Version, "")
	if err != nil || view.Profile.Status != domain.ListingPublished {
		t.Fatalf("reanudar: %v %v", view.Profile.Status, err)
	}
}

func TestProviderCalendar(t *testing.T) {
	f := newProviderFixture()
	ctx := context.Background()
	start := f.clock.now.Add(48 * time.Hour)
	block := domain.AvailabilityBlock{Start: start, End: start.Add(24 * time.Hour), Note: "Viaje"}
	if _, err := f.svc.BlockDates(ctx, "ana", block, ""); !errors.Is(err, domain.ErrProviderProfileRequired) {
		t.Fatalf("sin perfil: %v", err)
	}
	_, _ = f.svc.Activate(ctx, "ana", providerInput(), "")

	b, err := f.svc.BlockDates(ctx, "ana", block, "")
	if err != nil || b.ID == "" || b.Reason != domain.BlockManual {
		t.Fatalf("bloquear: %+v %v", b, err)
	}
	if _, err := f.svc.BlockDates(ctx, "ana", block, ""); !errors.Is(err, domain.ErrAvailabilityOverlap) {
		t.Fatalf("sin cruces: %v", err)
	}
	blocks, err := f.svc.Calendar(ctx, "ana", f.clock.now, f.clock.now.Add(30*24*time.Hour))
	if err != nil || len(blocks) != 1 {
		t.Fatalf("calendario: %+v %v", blocks, err)
	}
	if err := f.svc.UnblockDates(ctx, "ana", b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnblockDates(ctx, "ana", b.ID, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ya no existe: %v", err)
	}
}
