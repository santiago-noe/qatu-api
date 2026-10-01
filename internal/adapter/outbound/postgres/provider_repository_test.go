package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

func TestProviderRepository(t *testing.T) {
	db := newTestDB(t)
	repo := NewProviderRepository(db)
	ctx := context.Background()

	var userID, cityID, zoneA, zoneB, gasfiteria, pintura, rotomartillo string
	must(t, db.Pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('luis@qatu.pe', 'Luis') RETURNING id`).Scan(&userID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id, city_id FROM zones WHERE slug = 'ayacucho'`).Scan(&zoneA, &cityID))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM zones WHERE slug = 'carmen-alto'`).Scan(&zoneB))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE vertical = 'service' AND slug = 'gasfiteria'`).Scan(&gasfiteria))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE vertical = 'service' AND slug = 'pintura'`).Scan(&pintura))
	must(t, db.Pool.QueryRow(ctx, `SELECT id FROM categories WHERE slug = 'rotomartillo'`).Scan(&rotomartillo))

	if _, err := repo.FindProvider(ctx, userID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("aún no es proveedor: %v", err)
	}
	const pkgA, pkgB = "aaaaaaaa-0000-0000-0000-000000000001", "aaaaaaaa-0000-0000-0000-000000000002"
	p := domain.ProviderProfile{
		UserID: userID, Phone: "+51987654321", CityID: cityID, Bio: "Gasfitero de Huamanga", YearsExperience: 8,
		WarrantyDays: 15, Status: domain.ListingDraft,
		Trades: []domain.ProviderTrade{
			{CategoryID: gasfiteria, HourlyRate: 25_00, MinHours: 2, Packages: []domain.ServicePackage{
				{ID: pkgA, Title: "Cambio de grifería", Price: 60_00, DurationMinutes: 60},
				{ID: pkgB, Title: "Destape de desagüe", Price: 50_00, DurationMinutes: 90},
			}},
			{CategoryID: pintura, MinHours: 1},
		},
		CoverageZoneIDs: []string{zoneA, zoneB},
		Weekly:          []domain.WeeklySlot{{Weekday: 1, Start: 480, End: 720}, {Weekday: 1, Start: 840, End: 1080}},
	}
	consent := domain.Consent{Purpose: domain.ConsentProviderTerms, Version: "v1", GrantedAt: time.Now()}
	must(t, repo.CreateProvider(ctx, p, consent, domain.AuditEntry{ActorID: userID, Action: domain.AuditProviderActivated,
		Entity: "provider_profile", EntityID: userID, IP: "190.1.2.3"}))

	got, err := repo.FindProvider(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Trades) != 2 || got.Trades[0].CategoryID != gasfiteria || len(got.Trades[0].Packages) != 2 {
		t.Fatalf("perfil: %+v", got)
	}
	if !got.Trades[1].QuoteOnly() || len(got.CoverageZoneIDs) != 2 || len(got.Weekly) != 2 || got.WarrantyDays != 15 {
		t.Fatalf("detalles: %+v", got)
	}
	var consents int
	must(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM consents WHERE user_id = $1 AND purpose = 'provider_terms'`, userID).Scan(&consents))
	if consents != 1 {
		t.Fatalf("consentimiento: %d", consents)
	}

	// Editar: quita pintura y un paquete; el que sigue conserva su fila.
	var createdAt time.Time
	must(t, db.Pool.QueryRow(ctx, `SELECT created_at FROM service_packages WHERE id = $1`, pkgA).Scan(&createdAt))
	got.Trades = got.Trades[:1]
	got.Trades[0].Packages = got.Trades[0].Packages[:1]
	got.Trades[0].Packages[0].Price = 70_00
	got.CoverageZoneIDs = []string{zoneB}
	got.Weekly = []domain.WeeklySlot{{Weekday: 6, Start: 540, End: 780}}
	audit := domain.AuditEntry{ActorID: userID, Action: domain.AuditProviderUpdated, Entity: "provider_profile", EntityID: userID}
	saved, err := repo.UpdateProvider(ctx, got, 1, audit)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 2 || len(saved.Trades) != 1 || len(saved.Trades[0].Packages) != 1 || saved.Trades[0].Packages[0].Price != 70_00 {
		t.Fatalf("editar: %+v", saved)
	}
	var stillCreated time.Time
	must(t, db.Pool.QueryRow(ctx, `SELECT created_at FROM service_packages WHERE id = $1`, pkgA).Scan(&stillCreated))
	if !stillCreated.Equal(createdAt) {
		t.Fatal("el paquete que sigue no se vuelve a crear")
	}
	if len(saved.CoverageZoneIDs) != 1 || saved.Weekly[0].Weekday != 6 {
		t.Fatalf("cobertura y horario: %+v", saved)
	}
	if _, err := repo.UpdateProvider(ctx, got, 1, audit); !errors.Is(err, domain.ErrProviderVersion) {
		t.Fatalf("versión vieja: %v", err)
	}

	// La base también impide un tipo de herramienta como oficio y franjas que se cruzan.
	bad := saved
	bad.Trades = []domain.ProviderTrade{{CategoryID: rotomartillo, MinHours: 1}}
	if _, err := repo.UpdateProvider(ctx, bad, saved.Version, audit); !errors.Is(err, domain.ErrProviderTrade) {
		t.Fatalf("herramienta como oficio: %v", err)
	}
	bad = saved
	bad.Weekly = []domain.WeeklySlot{{Weekday: 2, Start: 480, End: 720}, {Weekday: 2, Start: 600, End: 900}}
	if _, err := repo.UpdateProvider(ctx, bad, saved.Version, audit); !errors.Is(err, domain.ErrProviderSchedule) {
		t.Fatalf("franjas cruzadas: %v", err)
	}

	// Publicado sin nivel P no se puede.
	bad = saved
	bad.Status = domain.ListingPublished
	if _, err := repo.UpdateProvider(ctx, bad, saved.Version, audit); err == nil {
		t.Fatal("publicado exige verified_at")
	}

	// Días bloqueados: sin cruces y solo se liberan los manuales.
	start := time.Date(2026, 11, 10, 5, 0, 0, 0, time.UTC)
	block := domain.AvailabilityBlock{Start: start, End: start.Add(48 * time.Hour), Reason: domain.BlockManual, Note: "Viaje", CreatedBy: userID}
	id, err := repo.AddProviderBlock(ctx, userID, block, domain.AuditEntry{ActorID: userID, Action: domain.AuditProviderBlocked, Entity: "provider_profile"})
	if err != nil {
		t.Fatal(err)
	}
	block.Start = start.Add(24 * time.Hour)
	if _, err := repo.AddProviderBlock(ctx, userID, block, domain.AuditEntry{}); !errors.Is(err, domain.ErrAvailabilityOverlap) {
		t.Fatalf("cruce: %v", err)
	}
	blocks, err := repo.ListProviderBlocks(ctx, userID, start.Add(-24*time.Hour), start.Add(30*24*time.Hour))
	if err != nil || len(blocks) != 1 || blocks[0].Note != "Viaje" {
		t.Fatalf("listar: %+v %v", blocks, err)
	}
	unblock := domain.AuditEntry{ActorID: userID, Action: domain.AuditProviderUnblocked, Entity: "provider_profile", EntityID: userID}
	must(t, repo.DeleteProviderManualBlock(ctx, userID, id, unblock))
	if err := repo.DeleteProviderManualBlock(ctx, userID, id, unblock); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ya no existe: %v", err)
	}
}
